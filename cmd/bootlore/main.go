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

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/journal"
	"github.com/ElenaFri/bootlore/internal/render"
)

// version is overridable at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run executes the CLI and returns a process exit code. It is kept separate from
// main so it can be exercised directly by tests.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "collect":
		return cmdCollect(args[1:], stdin, stdout, stderr)
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
  version    Print the BootLore version
  help       Show this help

collect flags:
  --journal        read directly from the system journal (journalctl)
  --boot OFFSET    which boot to read: 0 (current, default), -1 (previous), ...
  --input FILE     read captured journal JSON (journalctl -o json) from FILE

Examples:
  bootlore collect
  bootlore collect --boot -1
  journalctl -b -o json | bootlore collect
  bootlore collect --input boot.json
`)
}

func cmdCollect(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "read journal JSON (journalctl -o json) from this file")
	fromJournal := fs.Bool("journal", false, "read directly from the system journal (journalctl)")
	boot := fs.String("boot", "", "boot to read as a journalctl -b offset (default current boot)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	events, err := collectEvents(*input, *fromJournal, *boot, stdin)
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
		fmt.Fprintln(out, render.LogLine(e))
	}
	return 0
}

// collectEvents selects the event source and returns the normalized events.
// Precedence: --input file, then piped stdin, then the system journal.
func collectEvents(input string, fromJournal bool, boot string, stdin io.Reader) ([]event.Event, error) {
	switch {
	case input != "":
		f, err := os.Open(input)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return journal.ParseWithOrigin(f, event.OriginHistorical)

	case fromJournal || !stdinIsPiped(stdin):
		c := collector.New(collector.NewExecRunner())
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
