// Command bootlore is the command-line entry point for the BootLore boot
// observer. It turns systemd journal data into a readable account of system
// startup.
//
// The first implemented subcommand is "collect", which produces a chronological
// LOG view of a boot. It reads events from, in order of preference:
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
	"syscall"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/interpret"
	"github.com/ElenaFri/bootlore/internal/journal"
	"github.com/ElenaFri/bootlore/internal/knowledge"
	"github.com/ElenaFri/bootlore/internal/render"
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
  explain    Print the HUMAN view with contextual, educational explanations
  version    Print the BootLore version
  help       Show this help

collect flags:
  --journal        read directly from the system journal (journalctl)
  --boot OFFSET    which boot to read: 0 (current, default), -1 (previous), ...
  --input FILE     read captured journal JSON (journalctl -o json) from FILE
  --view V         event view: log (default, original messages) or human

live flags:
  --boot OFFSET    which boot to watch (default: current)
  --view V         event view: log (default) or human

explain flags:
  --journal        read directly from the system journal (journalctl)
  --boot OFFSET    which boot to read (default: current)
  --input FILE     read captured journal JSON (journalctl -o json) from FILE
  --level L        explanation depth: overview, technical (default) or in-depth

Examples:
  bootlore collect
  bootlore collect --boot -1 --view human
  bootlore live
  bootlore explain --level in-depth
  journalctl -b -o json | bootlore collect
  bootlore collect --input boot.json
`)
}

func cmdCollect(args []string, stdin io.Reader, stdout, stderr io.Writer, c *collector.Collector) int {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "read journal JSON (journalctl -o json) from this file")
	fromJournal := fs.Bool("journal", false, "read directly from the system journal (journalctl)")
	boot := fs.String("boot", "", "boot to read as a journalctl -b offset (default current boot)")
	view := fs.String("view", "log", "event view: log or human")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	line, err := viewRenderer(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	events, err := collectEvents(*input, *fromJournal, *boot, stdin, c)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		if errors.Is(err, collector.ErrBackendUnavailable) {
			fmt.Fprintln(stderr, "hint: pipe journal data instead, e.g. `journalctl -b -o json | bootlore collect`")
		}
		return 1
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for _, e := range events {
		fmt.Fprintln(out, line(interpret.Interpret(e)))
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
	if err := fs.Parse(args); err != nil {
		return 2
	}
	line, err := viewRenderer(*view)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	out := bufio.NewWriter(stdout)
	err = c.Watch(ctx, collector.Options{Boot: *boot}, func(ev event.Event) error {
		fmt.Fprintln(out, line(interpret.Interpret(ev)))
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
	input := fs.String("input", "", "read journal JSON (journalctl -o json) from this file")
	fromJournal := fs.Bool("journal", false, "read directly from the system journal (journalctl)")
	boot := fs.String("boot", "", "boot to read as a journalctl -b offset (default current boot)")
	levelName := fs.String("level", "technical", "explanation depth: overview, technical or in-depth")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	level, err := knowledge.ParseLevel(*levelName)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 2
	}

	events, err := collectEvents(*input, *fromJournal, *boot, stdin, c)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		if errors.Is(err, collector.ErrBackendUnavailable) {
			fmt.Fprintln(stderr, "hint: pipe journal data instead, e.g. `journalctl -b -o json | bootlore explain`")
		}
		return 1
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for _, ev := range events {
		e := interpret.Interpret(ev)
		fmt.Fprintln(out, render.HumanLine(e))
		if entry, ok := knowledge.For(e); ok {
			if text := entry.Text(level); text != "" {
				fmt.Fprintf(out, "  %s\n", text)
			}
		}
	}
	return 0
}
