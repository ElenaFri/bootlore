// Command bootlore is the command-line entry point for the BootLore boot
// observer. It turns systemd journal data into a readable account of system
// startup.
//
// Commands that read a boot (collect, timeline, explain, errors) take their
// events from, in order of preference:
//
//   - a captured export file (--input FILE);
//   - standard input, when journal JSON is piped in;
//   - the system journal directly, via journalctl (the default in a terminal,
//     or when forced with --journal).
//
// BootLore is read-only, non-intrusive and usable without root privileges; it
// only sees what the invoking user is allowed to read. For example:
//
//	bootlore collect                      # read the current boot from journald
//	journalctl -b -o json | bootlore collect
//	bootlore collect --input boot.json
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/compare"
	"github.com/ElenaFri/bootlore/internal/doctor"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/filter"
	"github.com/ElenaFri/bootlore/internal/interpret"
	"github.com/ElenaFri/bootlore/internal/journal"
	"github.com/ElenaFri/bootlore/internal/knowledge"
	"github.com/ElenaFri/bootlore/internal/render"
	"github.com/ElenaFri/bootlore/internal/report"
	"github.com/ElenaFri/bootlore/internal/services"
	"github.com/ElenaFri/bootlore/internal/summary"
	"github.com/ElenaFri/bootlore/internal/timing"
	"github.com/ElenaFri/bootlore/internal/tui"
	"github.com/ElenaFri/bootlore/internal/vt"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the CLI with the real journal collector and returns a process
// exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWith(args, stdin, stdout, stderr, collector.New(collector.NewExecRunner()))
}

// runWith executes the CLI with an injected collector. The seam lets tests
// exercise the whole command surface in-process, without journalctl.
func runWith(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "collect":
		return cmdCollect(args[1:], stdin, stdout, stderr, c)
	case "live":
		return cmdLive(args[1:], stdout, stderr, c)
	case "explain":
		return cmdExplain(args[1:], stdin, stdout, stderr, c)
	case "errors":
		return cmdErrors(args[1:], stdin, stdout, stderr, c)
	case "timeline":
		return cmdTimeline(args[1:], stdin, stdout, stderr, c)
	case "summary":
		return cmdSummary(args[1:], stdin, stdout, stderr, c)
	case "services":
		return cmdServices(args[1:], stdin, stdout, stderr, c)
	case "timing":
		return cmdTiming(args[1:], stdin, stdout, stderr, c)
	case "compare":
		return cmdCompare(args[1:], stdout, stderr, c)
	case "report":
		return cmdReport(args[1:], stdin, stdout, stderr, c)
	case "doctor":
		return cmdDoctor(args[1:], stdout, stderr, c)
	case "tui":
		return cmdTui(args[1:], stdin, stdout, stderr, c)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "bootlore %s\n", version)
		return 0
	case "help", "-h", "--help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "bootlore: unknown command %q\n", args[0])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `BootLore — a readable, real-time view of Linux boot logs.

Usage:
  bootlore <command> [flags]

Commands:
  collect    Print the LOG view of a boot (from journald, a file or stdin)
  live       Replay the current boot, then follow new events live (Ctrl-C stops)
  timeline   Show the boot as a chronological timeline (+offsets since boot)
  summary    Summarize a boot: counts, warnings, failures, timing, subsystems
  services   List failed systemd services and reconstructed startup timings
  timing     Report boot duration: systemd's startup summary, phases, milestones
  compare    Contrast two boots: duration, counts and service-failure changes
  report     Export a boot's full diagnosis (summary+timing+services) to a file
  doctor     Check system compatibility and journal permissions
  explain    Print the HUMAN view with contextual, educational explanations
  errors     Print only warnings and failures (HUMAN view by default)
  tui        Interactive boot monitor: views, navigation, filtering (needs a tty)
  version    Print the BootLore version
  help       Show this help

flags shared by collect, timeline, summary, services, explain and errors:
  --journal        read directly from the system journal (journalctl)
  --boot OFFSET    which boot to read: 0 (current, default), -1 (previous), ...
  --input FILE     read captured journal JSON (journalctl -o json) from FILE
  --category LIST  only these subsystems, comma-separated: kernel, hardware,
                   storage, network, systemd, security, desktop, other,
                   unclassified

collect flags:
  --view V         event view: log (default, original messages) or human

live flags:
  --boot OFFSET    which boot to watch (default: current)
  --view V         event view: log (default) or human
  --category LIST  only these subsystems (see above)

timeline and errors flags:
  --view V         event view: human (default) or log

tui flags:
  --boot OFFSET    which boot to watch (default: current)
  --category LIST  only these subsystems (see above); also editable in-session with /
  --view V         starting view: log (default) or human (switch with 1/2 in-session)
  --vt N           service mode: verify this tty (ttyN) was assigned to the process,
                   then run on it (never seize a terminal, exit 1 if busy/unavailable)

tui keys:
  1/2 switch LOG/HUMAN · Enter detail · Tab explanation level
  j/k, up/down navigate (scroll, in the detail) · g/G top/bottom · pgup/pgdn page
  / filter by category · Esc clear/back · q quit

timing flags (no --category: it would drop the startup summary line):
  --journal        read directly from the system journal (journalctl)
  --boot OFFSET    which boot to read (default current)
  --input FILE     read captured journal JSON (journalctl -o json) from FILE

compare flags (contrasts a later boot against an earlier baseline):
  --boot OFFSET          current (later) boot to read (default: 0)
  --against OFFSET       baseline (earlier) boot to read (default: -1)
  --input FILE           read the current boot's journal JSON from FILE
  --against-input FILE   read the baseline boot's journal JSON from FILE

report flags (no --category: a report covers the whole boot):
  --journal        read directly from the system journal (journalctl)
  --boot OFFSET    which boot to read (default current)
  --input FILE     read captured journal JSON (journalctl -o json) from FILE
  --format FMT     report format: text (default), markdown or json
  --output FILE    write the report to FILE instead of stdout

explain flags:
  --level L        explanation depth: overview, technical (default) or in-depth

doctor flags: none — doctor probes the live system directly

Examples:
  bootlore collect
  bootlore collect --boot -1 --view human
  bootlore live --category network
  bootlore timeline
  bootlore summary
  bootlore services
  bootlore timing
  bootlore compare
  bootlore report --format markdown
  bootlore report --format json --output boot-report.json
  bootlore doctor
  bootlore explain --level in-depth
  bootlore errors --category storage,hardware
  journalctl -b -o json | bootlore collect
  bootlore collect --input boot.json
`)
}

// readFlags holds the flags shared by the commands that read a whole boot:
// where the events come from, and which subsystems to keep.
type readFlags struct {
	input       *string
	fromJournal *bool
	boot        *string
	category    *string
}

// newReadFlags registers the shared source and filter flags on fs.
func newReadFlags(fs *flag.FlagSet) *readFlags {
	return &readFlags{
		input:       fs.String("input", "", "read journal JSON (journalctl -o json) from this file"),
		fromJournal: fs.Bool("journal", false, "read directly from the system journal (journalctl)"),
		boot:        fs.String("boot", "", "boot to read as a journalctl -b offset (default current boot)"),
		category:    fs.String("category", "", "only these subsystems, comma-separated (default: all)"),
	}
}

// events reads the boot, interprets every event and keeps those that pass the
// --category filter. On failure it reports the problem (with a hint when the
// journal backend is unavailable) on stderr and returns the exit code to use:
// 2 for a usage error (bad --category), 1 for a runtime failure.
func (r *readFlags) events(cmd string, stdin io.Reader, stderr io.Writer, c *collector.Collector) (events []event.Event, code int, ok bool) {
	flt, err := filter.ParseCategories(*r.category)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return nil, 2, false
	}

	raw, err := collectEvents(*r.input, *r.fromJournal, *r.boot, stdin, c)
	if err != nil {
		reportCollectError(cmd, err, stderr)
		return nil, 1, false
	}

	for _, e := range interpretAll(raw) {
		if flt.Match(e) {
			events = append(events, e)
		}
	}
	return events, 0, true
}

// reportCollectError prints a collection failure on stderr, adding a hint on how
// to pipe journal data when the journal backend itself is unavailable.
func reportCollectError(cmd string, err error, stderr io.Writer) {
	fmt.Fprintf(stderr, "bootlore: %v\n", err)
	if errors.Is(err, collector.ErrBackendUnavailable) {
		fmt.Fprintf(stderr, "hint: pipe journal data instead, e.g. `journalctl -b -o json | bootlore %s`\n", cmd)
	}
}

// warnUserOnlyJournal explains, on stderr, that only the caller's user-session
// journal was visible, so the system boot could not be measured, and points to
// the two usual remedies.
func warnUserOnlyJournal(stderr io.Writer) {
	fmt.Fprintln(stderr, "note: only your user-session journal is visible, so the system boot cannot be measured.")
	fmt.Fprintln(stderr, "hint: join the systemd-journal group (sudo usermod -aG systemd-journal $USER, then log back in),")
	fmt.Fprintln(stderr, "      or read the system journal directly: sudo journalctl -b -o json | bootlore timing")
}

func cmdCollect(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	read := newReadFlags(fs)
	view := fs.String("view", "log", "event view: log or human")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	line, err := viewRenderer(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	events, code, ok := read.events("collect", stdin, stderr, c)
	if !ok {
		return code
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for _, e := range events {
		fmt.Fprintln(out, line(e))
	}
	return 0
}

// viewRenderer maps a --view value onto the rendering function for one event.
// The empty value means the default (log).
func viewRenderer(view string) (func(event.Event) string, error) {
	switch view {
	case "", "log":
		return render.LogLine, nil
	case "human":
		return render.HumanLine, nil
	default:
		return nil, fmt.Errorf("unknown view %q (want log or human)", view)
	}
}

// tuiView maps a --view value onto the TUI's starting representation.
func tuiView(view string) (tui.View, error) {
	switch view {
	case "", "log":
		return tui.ViewLog, nil
	case "human":
		return tui.ViewHuman, nil
	default:
		return tui.ViewLog, fmt.Errorf("unknown view %q (want log or human)", view)
	}
}

// collectEvents selects the event source and returns the normalized events.
// Precedence: --input file, then piped stdin, then the system journal.
func collectEvents(input string, fromJournal bool, boot string, stdin io.Reader, c *collector.Collector) ([]event.Event, error) {
	switch {
	case input != "":
		f, err := os.Open(input)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return journal.ParseWithOrigin(f, event.OriginHistorical)

	case fromJournal || !stdinIsPiped(stdin):
		return c.Collect(context.Background(), collector.Options{Boot: boot})

	default:
		return journal.ParseWithOrigin(stdin, event.OriginHistorical)
	}
}

// cmdErrors prints only noteworthy events (warnings and failures) for a boot.
// It defaults to the HUMAN view: the command answers "what went wrong?", which
// is exactly what the interpreted view shows. When nothing noteworthy is
// found, it says so explicitly rather than printing nothing.
func cmdErrors(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("errors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	read := newReadFlags(fs)
	view := fs.String("view", "human", "event view: human (default) or log")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	line, err := viewRenderer(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	events, code, ok := read.events("errors", stdin, stderr, c)
	if !ok {
		return code
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	found := 0
	for _, e := range events {
		if !e.Significance.IsNoteworthy() {
			continue
		}
		found++
		fmt.Fprintln(out, line(e))
	}
	if found == 0 {
		fmt.Fprintln(out, "No warnings or failures found.")
	}
	return 0
}

// cmdTimeline shows a boot as a chronological timeline: every event is placed
// at its monotonic offset since boot (+1.200s), ordered chronologically. It
// defaults to the HUMAN view, like errors: both are diagnostic readings.
func cmdTimeline(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("timeline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	read := newReadFlags(fs)
	view := fs.String("view", "human", "event view: human (default) or log")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	body, err := bodyRenderer(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	events, code, ok := read.events("timeline", stdin, stderr, c)
	if !ok {
		return code
	}

	sort.SliceStable(events, func(i, j int) bool {
		return timelineOrdered(events[i], events[j])
	})

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for _, e := range events {
		fmt.Fprintln(out, render.TimelineLine(e, body))
	}
	return 0
}

// cmdServices diagnoses a boot's systemd services: the units that failed and
// the startup timings BootLore could reconstruct (slowest first). It honours the
// shared source and --category flags, so the diagnosis can be narrowed to, for
// example, only storage units.
func cmdServices(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("services", flag.ContinueOnError)
	fs.SetOutput(stderr)
	read := newReadFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	events, code, ok := read.events("services", stdin, stderr, c)
	if !ok {
		return code
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	fmt.Fprintln(out, services.Format(services.Analyze(events)))
	return 0
}

// cmdSummary aggregates a boot into a single high-level overview: how many
// events there were, how many warnings and failures, the observed timing and the
// breakdown by subsystem. It honours the shared source and --category flags, so
// the overview can be narrowed to, for example, only storage events.
func cmdSummary(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("summary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	read := newReadFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	events, code, ok := read.events("summary", stdin, stderr, c)
	if !ok {
		return code
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	fmt.Fprintln(out, summary.Format(summary.Summarize(events)))
	return 0
}

// cmdTiming reconstructs and reports a boot's duration and timing: systemd's
// authoritative startup summary when present, its per-phase breakdown, and the
// targets reached along the way. It works on raw (uninterpreted) events, since
// timing only reads messages and monotonic offsets, and it deliberately omits
// the --category filter: dropping the systemd summary line would corrupt the
// totals.
func cmdTiming(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("timing", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "read journal JSON (journalctl -o json) from this file")
	fromJournal := fs.Bool("journal", false, "read directly from the system journal (journalctl)")
	boot := fs.String("boot", "", "boot to read as a journalctl -b offset (default current boot)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	events, err := collectEvents(*input, *fromJournal, *boot, stdin, c)
	if err != nil {
		reportCollectError("timing", err, stderr)
		return 1
	}

	out := bufio.NewWriter(stdout)
	fmt.Fprintln(out, timing.Format(timing.Analyze(events)))
	out.Flush()
	if timing.OnlyUserSession(events) {
		warnUserOnlyJournal(stderr)
	}
	return 0
}

// cmdCompare contrasts two boots: a current (later) boot against an earlier
// baseline. By default both come from the journal -- the current boot (--boot,
// default 0) and the baseline (--against, default -1) -- so `bootlore compare`
// with no flags answers "how does this boot compare to the previous one?". Each
// side can instead be read from a captured file (--input, --against-input), which
// is also how journal-less environments test it. It does not read stdin: two
// boots cannot share one pipe.
func cmdCompare(args []string, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	boot := fs.String("boot", "0", "current (later) boot to read, as a journalctl -b offset")
	against := fs.String("against", "-1", "baseline (earlier) boot to read, as a journalctl -b offset")
	input := fs.String("input", "", "read the current boot's journal JSON from this file")
	againstInput := fs.String("against-input", "", "read the baseline boot's journal JSON from this file")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	current, err := collectBoot(*input, *boot, c)
	if err != nil {
		reportCompareError(err, stderr)
		return 1
	}
	baseline, err := collectBoot(*againstInput, *against, c)
	if err != nil {
		reportCompareError(err, stderr)
		return 1
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	fmt.Fprintln(out, compare.Format(compare.Compare(interpretAll(baseline), interpretAll(current))))
	return 0
}

// cmdReport assembles a boot's summary, timing and systemd-service diagnosis into
// a single diagnostic report and renders it in the chosen format: text (the
// default), markdown (shareable, for pasting into a bug report) or json (for
// tooling and archival). With --output it writes the report to a file instead of
// stdout, which is the intended way to export a diagnosis. Like timing it covers
// the whole boot and so takes no --category filter: a holistic diagnosis of a
// single subsystem would mislead, and filtering out events would corrupt the
// timing totals.
func cmdReport(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "read journal JSON (journalctl -o json) from this file")
	fromJournal := fs.Bool("journal", false, "read directly from the system journal (journalctl)")
	boot := fs.String("boot", "", "boot to read as a journalctl -b offset (default current boot)")
	format := fs.String("format", "text", "report format: text (default), markdown or json")
	output := fs.String("output", "", "write the report to this file instead of stdout")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	raw, err := collectEvents(*input, *fromJournal, *boot, stdin, c)
	if err != nil {
		reportCollectError("report", err, stderr)
		return 1
	}

	events := interpretAll(raw)
	meta := report.Meta{Tool: "bootlore", Version: version, Generated: time.Now().UTC()}
	rendered, err := report.Format(report.Build(meta, events), *format)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	if code := emitReport(rendered, *output, stdout, stderr); code != 0 {
		return code
	}
	if timing.OnlyUserSession(events) {
		warnUserOnlyJournal(stderr)
	}
	return 0
}

// emitReport writes a rendered report to a file (when output is set) or to
// stdout, each terminated by a newline. On a file export it confirms the path on
// stderr so stdout stays empty. It returns 0 on success, or 1 on a write error
// (already reported on stderr).
func emitReport(rendered, output string, stdout, stderr io.Writer) int {
	data := []byte(rendered + "\n")
	if output != "" {
		if err := os.WriteFile(output, data, 0o644); err != nil {
			fmt.Fprintf(stderr, "bootlore: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "bootlore: wrote report to %s\n", output)
		return 0
	}
	out := bufio.NewWriter(stdout)
	if _, err := out.Write(data); err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 1
	}
	out.Flush()
	return 0
}

// cmdDoctor probes the live system — journal backend, journal readability, boot
// history — and prints a compatibility and permissions check. It takes no event
// source: doctor examines the system BootLore would run on, never a captured
// boot. The exit code is 0 when every check passes (warnings allowed) and 1 as
// soon as one check fails.
func cmdDoctor(args []string, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx := context.Background()
	var f doctor.Findings
	events, err := c.Collect(ctx, collector.Options{})
	if err != nil {
		f.BackendErr = err
	} else {
		f.Events = events
		if n, berr := c.BootCount(ctx); berr == nil {
			f.BootCount = n
		}
	}

	checks := doctor.Analyze(f)
	out := bufio.NewWriter(stdout)
	fmt.Fprintln(out, "BootLore doctor — system compatibility and permissions check")
	fmt.Fprintln(out)
	fmt.Fprint(out, doctor.Render(checks))
	out.Flush()

	if doctor.Worst(checks) == doctor.Fail {
		return 1
	}
	return 0
}

// collectBoot reads one side of a comparison: a captured file when input is set,
// otherwise the given boot from the journal.
func collectBoot(input, boot string, c *collector.Collector) ([]event.Event, error) {
	if input != "" {
		f, err := os.Open(input)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return journal.ParseWithOrigin(f, event.OriginHistorical)
	}
	return c.Collect(context.Background(), collector.Options{Boot: boot})
}

// reportCompareError prints a collection failure for compare. Unlike the other
// commands it cannot suggest piping (a comparison needs two boots), so its hint
// points at capturing each boot to a file instead.
func reportCompareError(err error, stderr io.Writer) {
	fmt.Fprintf(stderr, "bootlore: %v\n", err)
	if errors.Is(err, collector.ErrBackendUnavailable) {
		fmt.Fprintln(stderr, "hint: capture each boot with `journalctl -b OFFSET -o json > boot.json`, then pass them with --input and --against-input")
	}
}

// interpretAll interprets every event in a boot, filling Category and
// Significance, without modifying the input slice.
func interpretAll(raw []event.Event) []event.Event {
	out := make([]event.Event, len(raw))
	for i, e := range raw {
		out[i] = interpret.Interpret(e)
	}
	return out
}

// bodyRenderer maps a --view value onto the body rendering function used by the
// timeline view (which prints its own prefix). The empty value means the
// default (log).
func bodyRenderer(view string) (func(event.Event) string, error) {
	switch view {
	case "", "log":
		return render.LogBody, nil
	case "human":
		return render.HumanBody, nil
	default:
		return nil, fmt.Errorf("unknown view %q (want log or human)", view)
	}
}

// timelineOrdered is the defensive chronological ordering of the timeline:
// monotonic time since boot is preferred (architecture §4.7); wall-clock time
// is the fallback when either event lacks a monotonic timestamp.
func timelineOrdered(a, b event.Event) bool {
	if a.MonotonicTimestamp != 0 && b.MonotonicTimestamp != 0 {
		return a.MonotonicTimestamp < b.MonotonicTimestamp
	}
	return a.RealtimeTimestamp.Before(b.RealtimeTimestamp)
}

// cmdTui runs the interactive boot monitor. It requires a real terminal on
// both stdin and stdout; without one (piped output, CI) it fails gracefully
// with exit code 1 instead of drawing garbage.
func cmdTui(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	boot := fs.String("boot", "", "boot to watch as a journalctl -b offset (default current boot)")
	category := fs.String("category", "", "only these subsystems, comma-separated (default: all)")
	view := fs.String("view", "log", "starting view: log (default) or human")
	vtNumber := fs.Int("vt", 0, "service mode: verify the given virtual terminal (ttyN) was assigned to this process, then run on it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := filter.ParseCategories(*category); err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}
	initialView, err := tuiView(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK || !tui.IsTerminalable(in, out) {
		fmt.Fprintln(stderr, "bootlore: "+tui.ErrNotATTY.Error())
		return 1
	}

	// Service mode: the terminal was handed to us (systemd TTYPath); never
	// seized. Verify it before touching the journal so a misconfigured unit
	// fails fast with a clear reason.
	if *vtNumber != 0 {
		st, err := in.Stat()
		if err != nil {
			fmt.Fprintf(stderr, "bootlore: cannot inspect the input terminal: %v\n", err)
			return 1
		}
		rdev := uint64(st.Sys().(*syscall.Stat_t).Rdev)
		if err := vt.NewProbe().RequireCurrent(rdev, *vtNumber); err != nil {
			fmt.Fprintf(stderr, "bootlore: --vt %d: %v\n", *vtNumber, err)
			return 1
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = tui.Run(ctx, in, out, c, collector.Options{Boot: *boot}, strings.TrimSpace(*category), initialView)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		if errors.Is(err, collector.ErrBackendUnavailable) {
			fmt.Fprintln(stderr, "hint: BootLore's TUI needs systemd's journalctl")
		}
		return 1
	}
	return 0
}

// stdinIsPiped reports whether stdin carries piped data rather than being an
// interactive terminal. Non-file readers (as used in tests) are treated as piped.
func stdinIsPiped(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return true
	}
	fi, err := f.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

// cmdLive replays the requested boot (historical events) and then follows the
// journal live, printing the selected view as events arrive. It stops cleanly
// on SIGINT/SIGTERM.
func cmdLive(args []string, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("live", flag.ContinueOnError)
	fs.SetOutput(stderr)
	boot := fs.String("boot", "", "boot to watch as a journalctl -b offset (default current boot)")
	view := fs.String("view", "log", "event view: log or human")
	category := fs.String("category", "", "only these subsystems, comma-separated (default: all)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	line, err := viewRenderer(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}
	flt, err := filter.ParseCategories(*category)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := bufio.NewWriter(stdout)
	err = c.Watch(ctx, collector.Options{Boot: *boot}, func(ev event.Event) error {
		e := interpret.Interpret(ev)
		if !flt.Match(e) {
			return nil
		}
		fmt.Fprintln(out, line(e))
		// Flush per event: live output must appear as it happens, not in
		// batches when the process eventually exits.
		return out.Flush()
	})
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		if errors.Is(err, collector.ErrBackendUnavailable) {
			fmt.Fprintln(stderr, "hint: BootLore's live mode needs systemd's journalctl")
		}
		return 1
	}
	return 0
}

// cmdExplain prints the HUMAN view of the boot, each event followed by its
// contextual explanation at the requested depth. Events without a knowledge
// entry are still listed (falling back to their original message), just
// without an explanation paragraph.
func cmdExplain(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("explain", flag.ContinueOnError)
	fs.SetOutput(stderr)
	read := newReadFlags(fs)
	levelName := fs.String("level", "technical", "explanation depth: overview, technical or in-depth")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	level, err := knowledge.ParseLevel(*levelName)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	events, code, ok := read.events("explain", stdin, stderr, c)
	if !ok {
		return code
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for _, e := range events {
		fmt.Fprintln(out, render.HumanLine(e))
		if entry, ok := knowledge.For(e); ok {
			if text := entry.Text(level); text != "" {
				fmt.Fprintf(out, "  %s\n", text)
			}
		}
	}
	return 0
}
