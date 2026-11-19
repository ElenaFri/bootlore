//go:build linux

package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
	"github.com/ElenaFri/bootlore/internal/ptytest"
)

const (
	nmEvent  = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","_SYSTEMD_UNIT":"NetworkManager.service","MESSAGE":"Started Network Manager."}`
	bootMsg  = `{"__REALTIME_TIMESTAMP":"1700000001000000","__MONOTONIC_TIMESTAMP":"2200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","MESSAGE":"Reached target Multi-User System."}`
	nmEvent2 = `{"__REALTIME_TIMESTAMP":"1700000002000000","__MONOTONIC_TIMESTAMP":"3200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"NetworkManager","_PID":"412","_SYSTEMD_UNIT":"NetworkManager.service","MESSAGE":"dhcp4 (eth0): state changed"}`
)

// session is a TUI run attached to a pseudo-terminal.
type session struct {
	pty    *ptytest.Pty
	done   chan error
	cancel context.CancelFunc
}

// start runs the TUI against the pty with the given journal backend and waits
// for the screen to be set up (which happens after the terminal is in raw mode,
// so it is safe to type from then on).
func start(t *testing.T, runner collector.Runner, initialFilter string) *session {
	t.Helper()
	return startSized(t, runner, initialFilter, 100, 12)
}

// startSized is start on a terminal of the given size.
func startSized(t *testing.T, runner collector.Runner, initialFilter string, cols, rows int) *session {
	t.Helper()
	p := ptytest.New(t, cols, rows)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s := &session{pty: p, done: make(chan error, 1), cancel: cancel}
	go func() {
		s.done <- Run(ctx, p.Slave, p.Slave, collector.New(runner), collector.Options{}, initialFilter)
	}()
	p.WaitFor(t, 0, enterScreen)
	return s
}

// finish waits for Run to return and gives back its error.
func (s *session) finish(t *testing.T) error {
	t.Helper()
	select {
	case err := <-s.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return in time")
		return nil
	}
}

func replayOf(lines ...string) *collectortest.SequenceRunner {
	return &collectortest.SequenceRunner{Outputs: []string{strings.Join(lines, "\n")}}
}

func TestRunShowsTheBootAndQuitsOnQ(t *testing.T) {
	s := start(t, replayOf(nmEvent, bootMsg), "")
	s.pty.WaitFor(t, 0, "Started Network Manager.")
	s.pty.WaitFor(t, 0, "Reached target Multi-User System.")
	s.pty.WaitFor(t, 0, "2/2 events")

	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatalf("Run returned %v after q", err)
	}
	// Run has returned, but the pty reader may not have caught up yet.
	s.pty.WaitFor(t, 0, leaveScreen) // the normal screen and the cursor are restored
}

func TestRunQuitsOnCtrlC(t *testing.T) {
	s := start(t, replayOf(nmEvent), "")
	s.pty.Type(t, "\x03")
	if err := s.finish(t); err != nil {
		t.Fatalf("Run returned %v after Ctrl-C", err)
	}
}

func TestRunSwitchesViewsAndInspectsAnEvent(t *testing.T) {
	s := start(t, replayOf(nmEvent), "")
	s.pty.WaitFor(t, 0, "Started Network Manager.")

	mark := s.pty.Mark()
	s.pty.Type(t, "2")
	s.pty.WaitFor(t, mark, "A systemd unit changed state as part of the boot sequence.")
	s.pty.WaitFor(t, mark, "HUMAN")

	mark = s.pty.Mark()
	s.pty.Type(t, "\r")
	s.pty.WaitFor(t, mark, "EXPLAIN [technical]")
	mark = s.pty.Mark()
	s.pty.Type(t, "\t")
	s.pty.WaitFor(t, mark, "EXPLAIN [in-depth]")

	mark = s.pty.Mark()
	s.pty.Type(t, "\x1b")
	s.pty.WaitFor(t, mark, "1/1 events") // back to the list

	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestRunFilterTypedAtTheKeyboard(t *testing.T) {
	s := start(t, replayOf(nmEvent, bootMsg), "")
	s.pty.WaitFor(t, 0, "2/2 events")

	mark := s.pty.Mark()
	s.pty.Type(t, "/network")
	s.pty.WaitFor(t, mark, "filter: network")

	mark = s.pty.Mark()
	s.pty.Type(t, "\r")
	s.pty.WaitFor(t, mark, "filter:network")
	s.pty.WaitFor(t, mark, "1/2 events")

	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

func TestRunAppliesTheInitialFilter(t *testing.T) {
	s := start(t, replayOf(nmEvent, bootMsg), "network")
	s.pty.WaitFor(t, 0, "filter:network")
	s.pty.WaitFor(t, 0, "1/2 events")
	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

// An invalid filter must be refused before the terminal is touched: nothing
// printed, no raw mode left behind.
func TestRunRejectsAnInvalidInitialFilterWithoutTouchingTheTerminal(t *testing.T) {
	p := ptytest.New(t, 100, 12)
	before := termios(t, p.Slave)

	err := Run(context.Background(), p.Slave, p.Slave, collector.New(replayOf(nmEvent)), collector.Options{}, "bogus")
	if err == nil || !strings.Contains(err.Error(), "unknown category") {
		t.Fatalf("Run error = %v, want an unknown-category error", err)
	}
	if out := p.Output(); out != "" {
		t.Errorf("nothing may be printed before the filter is validated, got %q", out)
	}
	if after := termios(t, p.Slave); after != before {
		t.Error("the terminal mode must be left untouched")
	}
}

func TestRunRestoresTheTerminalMode(t *testing.T) {
	p := ptytest.New(t, 100, 12)
	before := termios(t, p.Slave)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, p.Slave, p.Slave, collector.New(replayOf(nmEvent)), collector.Options{}, "")
	}()
	p.WaitFor(t, 0, enterScreen)
	if termios(t, p.Slave) == before {
		t.Fatal("precondition: the terminal should be in raw mode while running")
	}
	p.Type(t, "q")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	cancel()
	if after := termios(t, p.Slave); after != before {
		t.Errorf("the terminal mode must be restored on exit:\n before = %+v\n after  = %+v", before, after)
	}
}

func TestRunStopsWhenTheContextIsCanceled(t *testing.T) {
	s := start(t, replayOf(nmEvent), "")
	s.cancel()
	if err := s.finish(t); err != nil {
		t.Fatalf("Run returned %v on cancellation, want nil", err)
	}
}

// Closing the terminal (the user's window goes away) ends the key stream; the
// TUI must leave instead of spinning.
func TestRunStopsWhenTheTerminalCloses(t *testing.T) {
	s := start(t, replayOf(nmEvent), "")
	s.pty.Master.Close()
	if err := s.finish(t); err != nil {
		t.Fatalf("Run returned %v when the terminal closed, want nil", err)
	}
}

func TestRunReturnsABackendError(t *testing.T) {
	p := ptytest.New(t, 100, 12)
	runner := &collectortest.FakeRunner{Err: collector.ErrBackendUnavailable}

	err := Run(context.Background(), p.Slave, p.Slave, collector.New(runner), collector.Options{}, "")
	if !errors.Is(err, collector.ErrBackendUnavailable) {
		t.Fatalf("Run error = %v, want ErrBackendUnavailable", err)
	}
	p.WaitFor(t, 0, leaveScreen) // the terminal is restored before the error is reported
}

func TestRunOnANonTerminalFails(t *testing.T) {
	plain := regularFile(t)
	err := Run(context.Background(), plain, plain, collector.New(replayOf(nmEvent)), collector.Options{}, "")
	if err == nil {
		t.Error("Run on a regular file should fail")
	}
}

// When the journal stream ends the events stay browsable: the TUI says so and
// waits for the user, instead of closing under their fingers.
func TestRunStaysOpenWhenTheJournalEnds(t *testing.T) {
	s := start(t, replayOf(nmEvent), "")
	s.pty.WaitFor(t, 0, "journal stream ended")

	select {
	case err := <-s.done:
		t.Fatalf("Run returned (%v) on its own; it should wait for the user", err)
	case <-time.After(200 * time.Millisecond):
	}

	s.pty.Type(t, "\r") // still interactive
	s.pty.WaitFor(t, 0, "EXPLAIN [technical]")
	s.pty.Type(t, "\x1bq")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

// endsBadly serves one event and then fails when closed, like a journalctl
// that dies after the replay.
type endsBadly struct{}

func (endsBadly) Run(context.Context, []string) (io.ReadCloser, error) {
	return badCloser{strings.NewReader(nmEvent + "\n")}, nil
}

type badCloser struct{ io.Reader }

func (badCloser) Close() error { return errors.New("journalctl failed: boom") }

// Once events are on screen, a journal failure is reported in the status bar and
// they stay browsable; it must not tear the screen down.
func TestRunKeepsTheEventsWhenTheJournalFailsLate(t *testing.T) {
	s := start(t, endsBadly{}, "")
	s.pty.WaitFor(t, 0, "Started Network Manager.")
	s.pty.WaitFor(t, 0, "journal error:")
	s.pty.WaitFor(t, 0, "journalctl failed: boom")

	select {
	case err := <-s.done:
		t.Fatalf("Run returned (%v) on its own; the collected events should stay browsable", err)
	case <-time.After(200 * time.Millisecond):
	}
	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

// pipeRunner serves an empty replay and then a live stream the test feeds. The
// writer of the live stream is handed to the test through a channel, which is
// what makes the hand-over race-free.
type pipeRunner struct {
	calls int
	live  chan *io.PipeWriter
}

func newPipeRunner() *pipeRunner { return &pipeRunner{live: make(chan *io.PipeWriter, 1)} }

func (r *pipeRunner) Run(ctx context.Context, _ []string) (io.ReadCloser, error) {
	r.calls++
	if r.calls == 1 {
		return io.NopCloser(strings.NewReader("")), nil
	}
	pr, pw := io.Pipe()
	r.live <- pw
	go func() {
		<-ctx.Done()
		pw.Close()
	}()
	return pr, nil
}

// Events arriving after the replay are appended and drawn without any key
// press.
func TestRunDrawsLiveEventsAsTheyArrive(t *testing.T) {
	runner := newPipeRunner()
	s := start(t, runner, "")
	s.pty.WaitFor(t, 0, "No events yet")

	var live *io.PipeWriter
	select {
	case live = <-runner.live:
	case <-time.After(5 * time.Second):
		t.Fatal("the live stream was never opened")
	}

	mark := s.pty.Mark()
	if _, err := live.Write([]byte(nmEvent2 + "\n")); err != nil {
		t.Fatal(err)
	}
	s.pty.WaitFor(t, mark, "dhcp4 (eth0): state changed")
	s.pty.WaitFor(t, mark, "1/1 events")

	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

// A window resize is delivered as SIGWINCH: the TUI re-reads the size and
// redraws at the new width.
func TestRunRedrawsOnWindowResize(t *testing.T) {
	s := startSized(t, replayOf(nmEvent), "", 60, 12)
	s.pty.WaitFor(t, 0, "1/1 events")
	if strings.Contains(s.pty.Output(), "q:quit") {
		t.Fatal("precondition: a 60-column status bar is expected to be cut before its last words")
	}

	s.pty.Resize(t, 160, 12)
	mark := s.pty.Mark()
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	s.pty.WaitFor(t, mark, "q:quit")

	s.pty.Type(t, "q")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}

// Keys typed in a burst (a paste, a held arrow) are all processed, in order.
func TestRunProcessesKeyBursts(t *testing.T) {
	s := start(t, replayOf(nmEvent, bootMsg, nmEvent2), "")
	s.pty.WaitFor(t, 0, "3/3 events")

	mark := s.pty.Mark()
	s.pty.Type(t, "jj\r") // select the third event and open it
	s.pty.WaitFor(t, mark, "Event 3/3")

	s.pty.Type(t, "\x1bq")
	if err := s.finish(t); err != nil {
		t.Fatal(err)
	}
}
