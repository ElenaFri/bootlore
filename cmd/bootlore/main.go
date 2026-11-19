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

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/filter"
	"github.com/ElenaFri/bootlore/internal/interpret"
	"github.com/ElenaFri/bootlore/internal/journal"
	"github.com/ElenaFri/bootlore/internal/knowledge"
	"github.com/ElenaFri/bootlore/internal/render"
	"github.com/ElenaFri/bootlore/internal/services"
	"github.com/ElenaFri/bootlore/internal/summary"
	"github.com/ElenaFri/bootlore/internal/tui"
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

tui keys:
  1/2 switch LOG/HUMAN · Enter detail · Tab explanation level
  j/k, up/down navigate (scroll, in the detail) · g/G top/bottom · pgup/pgdn page
  / filter by category · Esc clear/back · q quit

explain flags:
  --level L        explanation depth: overview, technical (default) or in-depth

Examples:
  bootlore collect
  bootlore collect --boot -1 --view human
  bootlore live --category network
  bootlore timeline
  bootlore summary
  bootlore services
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
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		if errors.Is(err, collector.ErrBackendUnavailable) {
			fmt.Fprintf(stderr, "hint: pipe journal data instead, e.g. `journalctl -b -o json | bootlore %s`\n", cmd)
		}
		return nil, 1, false
	}

	for _, ev := range raw {
		e := interpret.Interpret(ev)
		if flt.Match(e) {
			events = append(events, e)
		}
	}
	return events, 0, true
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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if _, err := filter.ParseCategories(*category); err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	in, inOK := stdin.(*os.File)
	out, outOK := stdout.(*os.File)
	if !inOK || !outOK || !tui.IsTerminalable(in, out) {
		fmt.Fprintln(stderr, "bootlore: "+tui.ErrNotATTY.Error())
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := tui.Run(ctx, in, out, c, collector.Options{Boot: *boot}, strings.TrimSpace(*category))
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
