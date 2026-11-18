//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
	"github.com/ElenaFri/bootlore/internal/event"
)

// The real exec runner must invoke journalctl, read its JSON output and produce
// normalized historical events.
func TestExecRunnerCollectsFromStubJournalctl(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, `printf '%s\n' '{"MESSAGE":"stub line 1","PRIORITY":"6"}' '{"MESSAGE":"stub line 2","PRIORITY":"4"}'`)
	t.Setenv("PATH", dir)

	c := collector.New(collector.NewExecRunner())
	events, err := c.Collect(context.Background(), collector.Options{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	collectortest.AssertEvents(t, events, []collectortest.ExpectEvent{
		{Message: "stub line 1", Origin: event.OriginHistorical},
		{Message: "stub line 2", Origin: event.OriginHistorical},
	})
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
	dir := collectortest.WriteStubJournalctl(t, "echo 'Failed to add match: Permission denied' >&2\nexit 1\n")
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
	dir := collectortest.WriteStubJournalctl(t, "exit 7\n")
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

// A journalctl that exists and is executable but cannot be exec'd (invalid
// binary format) must surface as ErrBackendUnavailable, not a crash.
func TestExecRunnerStartFailsOnInvalidBinary(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "journalctl")
	// Executable bit set, but neither an ELF image nor a script (no shebang):
	// LookPath succeeds, exec fails.
	if err := os.WriteFile(bin, []byte("definitely not an executable image"), 0o755); err != nil {
		t.Fatalf("writing invalid binary: %v", err)
	}
	t.Setenv("PATH", dir)

	c := collector.New(collector.NewExecRunner())
	_, err := c.Collect(context.Background(), collector.Options{})
	if !errors.Is(err, collector.ErrBackendUnavailable) {
		t.Fatalf("error = %v, want ErrBackendUnavailable", err)
	}
	if !strings.Contains(err.Error(), "journalctl") {
		t.Errorf("error should mention journalctl: %q", err.Error())
	}
}

// --- live following (streaming exec runner) ---

// Follow must deliver events as they arrive and end cleanly when the context
// is canceled while the backend is still running.
func TestFollowDeliversAndStopsOnCancel(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, `printf '%s\n' '{"MESSAGE":"live one","__CURSOR":"c1","PRIORITY":"6"}'
exec sleep 30
`)
	t.Setenv("PATH", dir)

	ctx, cancel := context.WithCancel(context.Background())
	c := collector.New(collector.NewExecRunner())

	got := make(chan event.Event, 1)
	done := make(chan error, 1)
	go func() {
		err := c.Follow(ctx, collector.Options{}, func(e event.Event) error {
			got <- e
			return nil
		})
		done <- err
	}()

	select {
	case e := <-got:
		if e.Origin != event.OriginLive {
			t.Errorf("event origin = %q, want live", e.Origin)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not deliver the stub event in time")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Follow after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Follow did not return after cancel")
	}
}

// The replay → live transition must work against two real journalctl
// invocations: the stub distinguishes them by their arguments, like the real
// journalctl would (the live call carries --after-cursor).
func TestWatchSeamlessWithStubJournalctl(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, collectortest.StubReplayThenLive)
	t.Setenv("PATH", dir)

	c := collector.New(collector.NewExecRunner())
	var got []event.Event
	err := c.Watch(context.Background(), collector.Options{}, func(e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}

	collectortest.AssertEvents(t, got, []collectortest.ExpectEvent{
		{Message: "replayed 1", Origin: event.OriginHistorical},
		{Message: "replayed 2", Origin: event.OriginHistorical},
		{Message: "live event", Origin: event.OriginLive},
	})
}
