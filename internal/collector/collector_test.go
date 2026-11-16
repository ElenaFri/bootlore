package collector

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// fakeRunner is a test double for Runner. It records the args it was called with
// and returns a canned reader or error.
type fakeRunner struct {
	output   string
	err      error
	gotArgs  []string
	reader   *trackingReader
	closedOK bool
}

func (f *fakeRunner) Run(_ context.Context, args []string) (io.ReadCloser, error) {
	f.gotArgs = args
	if f.err != nil {
		return nil, f.err
	}
	f.reader = &trackingReader{Reader: strings.NewReader(f.output)}
	return f.reader, nil
}

type trackingReader struct {
	io.Reader
	closed bool
}

func (t *trackingReader) Close() error {
	t.closed = true
	return nil
}

func TestJournalArgsDefaultsToCurrentBoot(t *testing.T) {
	got := JournalArgs(Options{})
	want := []string{"-o", "json", "--no-pager", "-b", "0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(default) = %v, want %v", got, want)
	}
}

func TestJournalArgsWithBootOffset(t *testing.T) {
	got := JournalArgs(Options{Boot: "-1"})
	want := []string{"-o", "json", "--no-pager", "-b", "-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(boot=-1) = %v, want %v", got, want)
	}
}

func TestCollectParsesRunnerOutputAsHistorical(t *testing.T) {
	fr := &fakeRunner{output: `{"MESSAGE":"first","PRIORITY":"6"}
{"MESSAGE":"second","PRIORITY":"4"}`}
	c := New(fr)

	events, err := c.Collect(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Message != "first" || events[1].Message != "second" {
		t.Errorf("unexpected messages: %q, %q", events[0].Message, events[1].Message)
	}
	for i, e := range events {
		if e.Origin != event.OriginHistorical {
			t.Errorf("events[%d].Origin = %q, want historical", i, e.Origin)
		}
	}
	if !fr.reader.closed {
		t.Error("Collect should close the runner's reader")
	}
}

func TestCollectPassesBuiltArgsToRunner(t *testing.T) {
	fr := &fakeRunner{output: ""}
	c := New(fr)
	if _, err := c.Collect(context.Background(), Options{Boot: "-2"}); err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	want := []string{"-o", "json", "--no-pager", "-b", "-2"}
	if !reflect.DeepEqual(fr.gotArgs, want) {
		t.Errorf("runner received args %v, want %v", fr.gotArgs, want)
	}
}

func TestCollectPropagatesRunnerError(t *testing.T) {
	sentinel := errors.New("boom")
	c := New(&fakeRunner{err: sentinel})
	_, err := c.Collect(context.Background(), Options{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Collect error = %v, want wrapping of %v", err, sentinel)
	}
}

func TestCollectReturnsParseError(t *testing.T) {
	c := New(&fakeRunner{output: "{not json}"})
	_, err := c.Collect(context.Background(), Options{})
	if err == nil {
		t.Fatal("Collect should return an error on malformed journal output")
	}
}

// --- live following ---

// sequenceRunner returns a different output for each Run call, recording the
// args of every invocation. It models the two journalctl invocations of a
// replay-then-live transition.
type sequenceRunner struct {
	outputs []string
	calls   int
	gotArgs [][]string
}

func (s *sequenceRunner) Run(_ context.Context, args []string) (io.ReadCloser, error) {
	s.gotArgs = append(s.gotArgs, args)
	i := s.calls
	s.calls++
	if i >= len(s.outputs) {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return io.NopCloser(strings.NewReader(s.outputs[i])), nil
}

// blockingReader blocks in Read until its context is done, then reports EOF.
// Its Close reports an error, like a real killed process would.
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

type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ []string) (io.ReadCloser, error) {
	return &blockingReader{ctx: ctx}, nil
}

func TestJournalArgsFollow(t *testing.T) {
	got := JournalArgs(Options{Follow: true})
	want := []string{"-o", "json", "--no-pager", "-b", "0", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(follow) = %v, want %v", got, want)
	}
}

func TestJournalArgsAfterCursor(t *testing.T) {
	got := JournalArgs(Options{Follow: true, AfterCursor: "s=abc;i=42"})
	want := []string{"-o", "json", "--no-pager", "-b", "0", "--after-cursor=s=abc;i=42", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(after-cursor) = %v, want %v", got, want)
	}
}

func TestFollowTagsEventsLive(t *testing.T) {
	c := New(&fakeRunner{output: `{"MESSAGE":"live one","PRIORITY":"6"}`})
	var got []event.Event
	err := c.Follow(context.Background(), Options{}, func(e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Follow returned error: %v", err)
	}
	if len(got) != 1 || got[0].Origin != event.OriginLive {
		t.Fatalf("got %+v, want one live event", got)
	}
}

// Canceling the context is the normal way to stop following: the killed
// backend's error must be swallowed, not reported.
func TestFollowCancelIsNormalStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := New(blockingRunner{})

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := c.Follow(ctx, Options{}, func(event.Event) error { return nil })
	if err != nil {
		t.Errorf("Follow after cancel = %v, want nil", err)
	}
}

// A consumer error must stop following and propagate.
func TestFollowConsumerErrorStops(t *testing.T) {
	sentinel := errors.New("enough")
	c := New(&fakeRunner{output: `{"MESSAGE":"one"}
{"MESSAGE":"two"}`})
	var count int
	err := c.Follow(context.Background(), Options{}, func(event.Event) error {
		count++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Follow error = %v, want consumer sentinel", err)
	}
	if count != 1 {
		t.Errorf("consumer called %d times, want 1", count)
	}
}

// --- replay → live transition ---

// Watch must deliver the replay exactly once, then resume strictly after the
// last cursor seen: the live invocation gets --after-cursor, so no event is
// duplicated or missed.
func TestWatchSeamlessTransition(t *testing.T) {
	sr := &sequenceRunner{outputs: []string{
		`{"MESSAGE":"replayed 1","__CURSOR":"c1","PRIORITY":"6"}
{"MESSAGE":"replayed 2","__CURSOR":"c2","PRIORITY":"6"}`, // replay
		`{"MESSAGE":"live 1","__CURSOR":"c3","PRIORITY":"6"}`, // live
	}}
	c := New(sr)

	var got []event.Event
	err := c.Watch(context.Background(), Options{}, func(e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}

	wantMessages := []string{"replayed 1", "replayed 2", "live 1"}
	wantOrigins := []event.Origin{event.OriginHistorical, event.OriginHistorical, event.OriginLive}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3: %+v", len(got), got)
	}
	for i := range wantMessages {
		if got[i].Message != wantMessages[i] {
			t.Errorf("got[%d].Message = %q, want %q", i, got[i].Message, wantMessages[i])
		}
		if got[i].Origin != wantOrigins[i] {
			t.Errorf("got[%d].Origin = %q, want %q", i, got[i].Origin, wantOrigins[i])
		}
	}

	if len(sr.gotArgs) != 2 {
		t.Fatalf("runner called %d times, want 2", len(sr.gotArgs))
	}
	liveArgs := sr.gotArgs[1]
	if !reflect.DeepEqual(liveArgs, []string{"-o", "json", "--no-pager", "-b", "0", "--after-cursor=c2", "-f"}) {
		t.Errorf("live args = %v, want after-cursor=c2 and -f", liveArgs)
	}
}

// With an empty replay (nothing in the journal yet), the live phase has no
// cursor to resume from and falls back to a plain follow.
func TestWatchEmptyReplayFallsBackToFollow(t *testing.T) {
	sr := &sequenceRunner{outputs: []string{
		"", // replay finds nothing
		`{"MESSAGE":"live 1","__CURSOR":"c1","PRIORITY":"6"}`,
	}}
	c := New(sr)

	var got []event.Event
	err := c.Watch(context.Background(), Options{}, func(e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}
	if len(got) != 1 || got[0].Origin != event.OriginLive {
		t.Fatalf("got %+v, want one live event", got)
	}
	liveArgs := sr.gotArgs[1]
	for _, a := range liveArgs {
		if strings.HasPrefix(a, "--after-cursor") {
			t.Errorf("live args %v should not contain --after-cursor on empty replay", liveArgs)
		}
	}
}

// Canceling during the replay phase must also end Watch cleanly.
func TestWatchCancelDuringReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := New(blockingRunner{})

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := c.Watch(ctx, Options{}, func(event.Event) error { return nil })
	if err != nil {
		t.Errorf("Watch after cancel = %v, want nil", err)
	}
}

func TestWatchReplayErrorPropagates(t *testing.T) {
	c := New(&fakeRunner{err: errors.New("boom")})
	err := c.Watch(context.Background(), Options{}, func(event.Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Watch error = %v, want propagated replay error", err)
	}
}
