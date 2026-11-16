// Command bootlore is the command-line entry point for the BootLore boot
// observer. It turns systemd journal data into a readable account of system
// startup.
//
// The first implemented subcommand is "collect", which reads journal entries in
// `journalctl -o json` format (from a file or standard input) and prints them as
// a chronological LOG view. This keeps BootLore read-only, non-intrusive and
// usable without root privileges, for example:
//
//	journalctl -b -o json | bootlore collect
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"

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
  collect    Read journal JSON (journalctl -o json) and print the LOG view
  version    Print the BootLore version
  help       Show this help

Examples:
  journalctl -b -o json | bootlore collect
  bootlore collect --input boot.json
`)
}

func cmdCollect(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("collect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	input := fs.String("input", "", "read journal JSON from this file instead of stdin")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var src io.Reader = stdin
	if *input != "" {
		f, err := os.Open(*input)
		if err != nil {
			fmt.Fprintf(stderr, "bootlore: %v\n", err)
			return 1
		}
		defer f.Close()
		src = f
	}

	// Reading an existing journal export is historical replay, not live capture.
	events, err := journal.ParseWithOrigin(src, event.OriginHistorical)
	if err != nil {
		fmt.Fprintf(stderr, "bootlore: %v\n", err)
		return 1
	}

	out := bufio.NewWriter(stdout)
	defer out.Flush()
	for _, e := range events {
		fmt.Fprintln(out, render.LogLine(e))
	}
	return 0
}
