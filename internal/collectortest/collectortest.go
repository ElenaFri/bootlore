// Package collectortest provides shared test doubles and helpers for exercising
// BootLore's journal collector across all test tiers (unit, integration and
// system). It is imported only by test files; it is never linked into the
// production binary.
package collectortest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
)

// FakeRunner is an in-memory collector.Runner. It records the arguments of every
// Run call and serves Output (or Err) in response. The reader it returns tracks
// whether it was closed.
type FakeRunner struct {
	Output  string
	Err     error
	GotArgs [][]string
	closed  bool
}

var _ collector.Runner = (*FakeRunner)(nil)

func (f *FakeRunner) Run(_ context.Context, args []string) (io.ReadCloser, error) {
	f.GotArgs = append(f.GotArgs, args)
	if f.Err != nil {
		return nil, f.Err
	}
	return &closeTracker{Reader: strings.NewReader(f.Output), flag: &f.closed}, nil
}

// Closed reports whether the reader from the latest successful Run was closed.
func (f *FakeRunner) Closed() bool { return f.closed }

// LastArgs returns the arguments passed to the most recent Run call.
func (f *FakeRunner) LastArgs() []string {
	if len(f.GotArgs) == 0 {
		return nil
	}
	return f.GotArgs[len(f.GotArgs)-1]
}

type closeTracker struct {
	io.Reader
	flag *bool
}

func (c *closeTracker) Close() error {
	*c.flag = true
	return nil
}

// SequenceRunner returns a different Output for each Run call, recording the
// args of every invocation. It models the two journalctl invocations of a
// replay-then-live transition. Calls beyond len(Outputs) return empty output.
type SequenceRunner struct {
	Outputs []string
	GotArgs [][]string
	calls   int
}

var _ collector.Runner = (*SequenceRunner)(nil)

func (s *SequenceRunner) Run(_ context.Context, args []string) (io.ReadCloser, error) {
	s.GotArgs = append(s.GotArgs, args)
	i := s.calls
	s.calls++
	if i >= len(s.Outputs) {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return io.NopCloser(strings.NewReader(s.Outputs[i])), nil
}

// BlockingRunner returns a reader that blocks until the context is canceled,
// then reports EOF. Its Close reports an error, like a real killed process.
// It models journalctl -f waiting for new events.
type BlockingRunner struct{}

var _ collector.Runner = BlockingRunner{}

func (BlockingRunner) Run(ctx context.Context, _ []string) (io.ReadCloser, error) {
	return &blockingReader{ctx: ctx}, nil
}

type blockingReader struct {
	ctx context.Context
}

func (b *blockingReader) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, io.EOF
}

func (b *blockingReader) Close() error {
	return errors.New("signal: killed")
}

// WriteStubJournalctl creates an executable `journalctl` stub in a fresh temp
// directory from the given shell body and returns that directory, suitable for
// use as the only PATH entry.
func WriteStubJournalctl(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("writing stub journalctl: %v", err)
	}
	return dir
}

// StubReplayThenLive is a journalctl stub body that emits two historical entries
// on a plain invocation and one live entry when asked to resume after a cursor
// (--after-cursor), modelling the replay→live transition driven by arguments.
const StubReplayThenLive = `case "$*" in
  *--after-cursor*)
    printf '%s\n' '{"MESSAGE":"live event","__CURSOR":"c3","PRIORITY":"6"}'
    ;;
  *)
    printf '%s\n' '{"MESSAGE":"replayed 1","__CURSOR":"c1","PRIORITY":"6"}' '{"MESSAGE":"replayed 2","__CURSOR":"c2","PRIORITY":"6"}'
    ;;
esac
`
