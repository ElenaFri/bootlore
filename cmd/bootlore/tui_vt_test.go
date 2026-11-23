package main

import (
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
	"github.com/ElenaFri/bootlore/internal/ptytest"
)

// --- tui --vt N (service mode: the terminal is given, never seized) ---

// A pty is not a virtual terminal: with --vt N the TUI must verify that the
// terminal it was handed is really ttyN and refuse otherwise, before drawing
// anything.
func TestTuiVtRejectsNonVTInput(t *testing.T) {
	p := ptytest.New(t, 100, 12)
	fr := &collectortest.FakeRunner{Err: collector.ErrBackendUnavailable}
	var errb strings.Builder
	code := runWith([]string{"tui", "--vt", "9"}, p.Slave, p.Slave, &errb, collector.New(fr))
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "virtual terminal 9") {
		t.Errorf("stderr should explain the terminal mismatch, got %q", errb.String())
	}
	// The journal backend must not even be consulted: the terminal check
	// comes first.
	if len(fr.GotArgs) != 0 {
		t.Errorf("the backend was called (%v); the tty check must come first", fr.LastArgs())
	}
}

func TestTuiVtRejectsBadNumber(t *testing.T) {
	p := ptytest.New(t, 100, 12)
	var errb strings.Builder
	code := runWith([]string{"tui", "--vt", "99"}, p.Slave, p.Slave, &errb, collector.New(&collectortest.FakeRunner{}))
	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "1..63") {
		t.Errorf("stderr should explain the valid range, got %q", errb.String())
	}
}
