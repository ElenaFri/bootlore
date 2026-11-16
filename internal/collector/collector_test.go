package collector

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

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
