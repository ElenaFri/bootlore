package collector_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
	"github.com/ElenaFri/bootlore/internal/event"
)

func TestJournalArgsDefaultsToCurrentBoot(t *testing.T) {
	got := collector.JournalArgs(collector.Options{})
	want := []string{"-o", "json", "--no-pager", "-b", "0"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(default) = %v, want %v", got, want)
	}
}

func TestJournalArgsWithBootOffset(t *testing.T) {
	got := collector.JournalArgs(collector.Options{Boot: "-1"})
	want := []string{"-o", "json", "--no-pager", "-b", "-1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(boot=-1) = %v, want %v", got, want)
	}
}

func TestCollectParsesRunnerOutputAsHistorical(t *testing.T) {
	fr := &collectortest.FakeRunner{Output: `{"MESSAGE":"first","PRIORITY":"6"}
{"MESSAGE":"second","PRIORITY":"4"}`}
	c := collector.New(fr)

	events, err := c.Collect(context.Background(), collector.Options{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	collectortest.AssertEvents(t, events, []collectortest.ExpectEvent{
		{Message: "first", Origin: event.OriginHistorical},
		{Message: "second", Origin: event.OriginHistorical},
	})
	if !fr.Closed() {
		t.Error("Collect should close the runner's reader")
	}
}

func TestCollectPassesBuiltArgsToRunner(t *testing.T) {
	fr := &collectortest.FakeRunner{}
	c := collector.New(fr)
	if _, err := c.Collect(context.Background(), collector.Options{Boot: "-2"}); err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	want := []string{"-o", "json", "--no-pager", "-b", "-2"}
	if !reflect.DeepEqual(fr.LastArgs(), want) {
		t.Errorf("runner received args %v, want %v", fr.LastArgs(), want)
	}
}

func TestCollectPropagatesRunnerError(t *testing.T) {
	sentinel := errors.New("boom")
	c := collector.New(&collectortest.FakeRunner{Err: sentinel})
	_, err := c.Collect(context.Background(), collector.Options{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Collect error = %v, want wrapping of %v", err, sentinel)
	}
}

func TestCollectReturnsParseError(t *testing.T) {
	c := collector.New(&collectortest.FakeRunner{Output: "{not json}"})
	_, err := c.Collect(context.Background(), collector.Options{})
	if err == nil {
		t.Fatal("Collect should return an error on malformed journal output")
	}
}

// --- live following ---

func TestJournalArgsFollow(t *testing.T) {
	got := collector.JournalArgs(collector.Options{Follow: true})
	want := []string{"-o", "json", "--no-pager", "-b", "0", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(follow) = %v, want %v", got, want)
	}
}

func TestJournalArgsAfterCursor(t *testing.T) {
	got := collector.JournalArgs(collector.Options{Follow: true, AfterCursor: "s=abc;i=42"})
	want := []string{"-o", "json", "--no-pager", "-b", "0", "--after-cursor=s=abc;i=42", "-f"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JournalArgs(after-cursor) = %v, want %v", got, want)
	}
}

func TestFollowTagsEventsLive(t *testing.T) {
	c := collector.New(&collectortest.FakeRunner{Output: `{"MESSAGE":"live one","PRIORITY":"6"}`})
	var got []event.Event
	err := c.Follow(context.Background(), collector.Options{}, func(e event.Event) error {
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
	c := collector.New(collectortest.BlockingRunner{})

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := c.Follow(ctx, collector.Options{}, func(event.Event) error { return nil })
	if err != nil {
		t.Errorf("Follow after cancel = %v, want nil", err)
	}
}

// A consumer error must stop following and propagate.
func TestFollowConsumerErrorStops(t *testing.T) {
	sentinel := errors.New("enough")
	c := collector.New(&collectortest.FakeRunner{Output: `{"MESSAGE":"one"}
{"MESSAGE":"two"}`})
	var count int
	err := c.Follow(context.Background(), collector.Options{}, func(event.Event) error {
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
	sr := &collectortest.SequenceRunner{Outputs: []string{
		`{"MESSAGE":"replayed 1","__CURSOR":"c1","PRIORITY":"6"}
{"MESSAGE":"replayed 2","__CURSOR":"c2","PRIORITY":"6"}`, // replay
		`{"MESSAGE":"live 1","__CURSOR":"c3","PRIORITY":"6"}`, // live
	}}
	c := collector.New(sr)

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
		{Message: "live 1", Origin: event.OriginLive},
	})

	if len(sr.GotArgs) != 2 {
		t.Fatalf("runner called %d times, want 2", len(sr.GotArgs))
	}
	liveArgs := sr.GotArgs[1]
	if !reflect.DeepEqual(liveArgs, []string{"-o", "json", "--no-pager", "-b", "0", "--after-cursor=c2", "-f"}) {
		t.Errorf("live args = %v, want after-cursor=c2 and -f", liveArgs)
	}
}

// With an empty replay (nothing in the journal yet), the live phase has no
// cursor to resume from and falls back to a plain follow.
func TestWatchEmptyReplayFallsBackToFollow(t *testing.T) {
	sr := &collectortest.SequenceRunner{Outputs: []string{
		"", // replay finds nothing
		`{"MESSAGE":"live 1","__CURSOR":"c1","PRIORITY":"6"}`,
	}}
	c := collector.New(sr)

	var got []event.Event
	err := c.Watch(context.Background(), collector.Options{}, func(e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("Watch returned error: %v", err)
	}
	if len(got) != 1 || got[0].Origin != event.OriginLive {
		t.Fatalf("got %+v, want one live event", got)
	}
	liveArgs := sr.GotArgs[1]
	for _, a := range liveArgs {
		if strings.HasPrefix(a, "--after-cursor") {
			t.Errorf("live args %v should not contain --after-cursor on empty replay", liveArgs)
		}
	}
}

// Canceling during the replay phase must also end Watch cleanly.
func TestWatchCancelDuringReplay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := collector.New(collectortest.BlockingRunner{})

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := c.Watch(ctx, collector.Options{}, func(event.Event) error { return nil })
	if err != nil {
		t.Errorf("Watch after cancel = %v, want nil", err)
	}
}

func TestWatchReplayErrorPropagates(t *testing.T) {
	c := collector.New(&collectortest.FakeRunner{Err: errors.New("boom")})
	err := c.Watch(context.Background(), collector.Options{}, func(event.Event) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Watch error = %v, want propagated replay error", err)
	}
}
