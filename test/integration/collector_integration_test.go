//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
)

// writeStubJournalctl creates an executable `journalctl` stub in a fresh temp
// directory and returns that directory. The script body is provided by caller.
func writeStubJournalctl(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "journalctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	return dir
}

// The real exec runner must invoke journalctl, read its JSON output and produce
// normalized historical events.
func TestExecRunnerCollectsFromStubJournalctl(t *testing.T) {
	dir := writeStubJournalctl(t, `printf '%s\n' '{"MESSAGE":"stub line 1","PRIORITY":"6"}' '{"MESSAGE":"stub line 2","PRIORITY":"4"}'`)
	t.Setenv("PATH", dir)

	c := collector.New(collector.NewExecRunner())
	events, err := c.Collect(context.Background(), collector.Options{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Message != "stub line 1" || events[1].Message != "stub line 2" {
		t.Errorf("unexpected messages: %q, %q", events[0].Message, events[1].Message)
	}
	if events[0].Origin != event.OriginHistorical {
		t.Errorf("Origin = %q, want historical", events[0].Origin)
	}
	if events[1].Priority != event.PriorityWarning {
		t.Errorf("second event Priority = %v, want warning", events[1].Priority)
	}
}

// A missing journalctl must surface as ErrBackendUnavailable, not a crash.
func TestExecRunnerBackendNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: no journalctl anywhere
	c := collector.New(collector.NewExecRunner())
	_, err := c.Collect(context.Background(), collector.Options{})
	if !errors.Is(err, collector.ErrBackendUnavailable) {
		t.Fatalf("error = %v, want ErrBackendUnavailable", err)
	}
}

// A failing journalctl (e.g. permission denied) must surface gracefully and
// carry the backend's own message.
func TestExecRunnerBackendFailure(t *testing.T) {
	dir := writeStubJournalctl(t, "echo 'Failed to add match: Permission denied' >&2\nexit 1\n")
	t.Setenv("PATH", dir)

	c := collector.New(collector.NewExecRunner())
	_, err := c.Collect(context.Background(), collector.Options{})
	if !errors.Is(err, collector.ErrBackendUnavailable) {
		t.Fatalf("error = %v, want ErrBackendUnavailable", err)
	}
	if got := err.Error(); !strings.Contains(got, "Permission denied") {
		t.Errorf("error %q should surface the backend stderr", got)
	}
}

// A failing journalctl that prints nothing on stderr must still produce a
// clear error, falling back to the exit status.
func TestExecRunnerBackendFailureSilent(t *testing.T) {
	dir := writeStubJournalctl(t, "exit 7\n")
	t.Setenv("PATH", dir)

	c := collector.New(collector.NewExecRunner())
	_, err := c.Collect(context.Background(), collector.Options{})
	if !errors.Is(err, collector.ErrBackendUnavailable) {
		t.Fatalf("error = %v, want ErrBackendUnavailable", err)
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Errorf("error should include the exit status: %q", err.Error())
	}
}
