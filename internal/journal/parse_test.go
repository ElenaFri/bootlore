package journal

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// A realistic single journal entry as produced by `journalctl -o json`.
const sampleEntry = `{"__CURSOR":"s=abc;i=1","__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1500000","_BOOT_ID":"2f8a","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_COMM":"systemd","_PID":"1","_SYSTEMD_UNIT":"NetworkManager.service","MESSAGE":"Started Network Manager."}`

func TestParseSingleEntry(t *testing.T) {
	events, err := Parse(strings.NewReader(sampleEntry))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}

	e := events[0]
	if e.Message != "Started Network Manager." {
		t.Errorf("Message = %q", e.Message)
	}
	if e.Priority != event.PriorityInformational {
		t.Errorf("Priority = %v, want info", e.Priority)
	}
	if e.Source != "systemd" {
		t.Errorf("Source = %q, want systemd", e.Source)
	}
	if e.PID != 1 {
		t.Errorf("PID = %d, want 1", e.PID)
	}
	if e.Unit != "NetworkManager.service" {
		t.Errorf("Unit = %q", e.Unit)
	}
	if e.BootID != "2f8a" {
		t.Errorf("BootID = %q", e.BootID)
	}
	if e.Cursor != "s=abc;i=1" {
		t.Errorf("Cursor = %q", e.Cursor)
	}

	wantRealtime := time.Unix(0, 1700000000000000*1000).UTC()
	if !e.RealtimeTimestamp.Equal(wantRealtime) {
		t.Errorf("RealtimeTimestamp = %v, want %v", e.RealtimeTimestamp, wantRealtime)
	}
	if e.MonotonicTimestamp != 1500000*time.Microsecond {
		t.Errorf("MonotonicTimestamp = %v, want 1.5s", e.MonotonicTimestamp)
	}
}

func TestParseMultipleEntriesPreservesOrder(t *testing.T) {
	in := `{"MESSAGE":"first","__MONOTONIC_TIMESTAMP":"100"}
{"MESSAGE":"second","__MONOTONIC_TIMESTAMP":"200"}
{"MESSAGE":"third","__MONOTONIC_TIMESTAMP":"300"}`

	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	want := []string{"first", "second", "third"}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for i, w := range want {
		if events[i].Message != w {
			t.Errorf("events[%d].Message = %q, want %q", i, events[i].Message, w)
		}
	}
}

// journald encodes non-UTF-8 messages as an array of byte values.
func TestParseMessageAsByteArray(t *testing.T) {
	// "Hi" encoded as [72, 105].
	in := `{"MESSAGE":[72,105]}`
	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 || events[0].Message != "Hi" {
		t.Fatalf("Message = %q, want %q", events[0].Message, "Hi")
	}
}

// journald encodes repeated fields as an array of strings; they are joined.
func TestParseMessageAsStringArray(t *testing.T) {
	in := `{"MESSAGE":["first part","second part"]}`
	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 || events[0].Message != "first part\nsecond part" {
		t.Fatalf("Message = %q, want two joined parts", events[0].Message)
	}
}

// A MESSAGE that cannot be decoded (e.g. a bare number) is a structural
// problem: parsing must fail rather than silently drop the message.
func TestParseUndecodableMessageReturnsError(t *testing.T) {
	in := `{"MESSAGE":123}`
	if _, err := Parse(strings.NewReader(in)); err == nil {
		t.Fatal("Parse should return an error for an undecodable MESSAGE")
	}
}

// Non-promoted fields with undecodable values are skipped without failing the
// whole entry; the rest of the event is preserved.
func TestParseSkipsUndecodableExtraField(t *testing.T) {
	in := `{"MESSAGE":"ok","_SOURCE_REALTIME_TIMESTAMP":123}`
	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 1 || events[0].Message != "ok" {
		t.Fatalf("unexpected events: %+v", events)
	}
	if _, present := events[0].Fields["_SOURCE_REALTIME_TIMESTAMP"]; present {
		t.Error("undecodable field should be absent from Fields")
	}
}

// Promoted fields with undecodable values degrade to their zero value instead
// of failing the entry.
func TestParseUndecodablePromotedFieldDegrades(t *testing.T) {
	in := `{"MESSAGE":"ok","_PID":123}`
	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if events[0].PID != 0 {
		t.Errorf("PID = %d, want 0 (undecodable _PID degrades)", events[0].PID)
	}
}

func TestParsePrefersSyslogIdentifierOverComm(t *testing.T) {
	in := `{"MESSAGE":"x","SYSLOG_IDENTIFIER":"kernel","_COMM":"swapper/0"}`
	events, _ := Parse(strings.NewReader(in))
	if events[0].Source != "kernel" {
		t.Errorf("Source = %q, want kernel (SYSLOG_IDENTIFIER preferred)", events[0].Source)
	}

	in = `{"MESSAGE":"x","_COMM":"swapper/0"}`
	events, _ = Parse(strings.NewReader(in))
	if events[0].Source != "swapper/0" {
		t.Errorf("Source = %q, want swapper/0 (fallback to _COMM)", events[0].Source)
	}
}

// Missing or invalid optional fields must be tolerated: the event is still
// produced, priority falls back to unknown, and no error is returned.
func TestParseToleratesMissingAndInvalidFields(t *testing.T) {
	in := `{"MESSAGE":"minimal"}
{"MESSAGE":"bad priority","PRIORITY":"99"}`
	events, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for i, e := range events {
		if e.Priority != event.PriorityUnknown {
			t.Errorf("events[%d].Priority = %v, want unknown", i, e.Priority)
		}
	}
}

// Any field that is not promoted to a dedicated Event field must be preserved
// in Fields so that no original information is lost.
func TestParsePreservesExtraFields(t *testing.T) {
	in := `{"MESSAGE":"x","_HOSTNAME":"thinkpad","_TRANSPORT":"journal"}`
	events, _ := Parse(strings.NewReader(in))
	e := events[0]
	if e.Fields["_HOSTNAME"] != "thinkpad" {
		t.Errorf("Fields[_HOSTNAME] = %q, want thinkpad", e.Fields["_HOSTNAME"])
	}
	if e.Fields["_TRANSPORT"] != "journal" {
		t.Errorf("Fields[_TRANSPORT] = %q, want journal", e.Fields["_TRANSPORT"])
	}
	// Promoted fields should not be duplicated into Fields.
	if _, dup := e.Fields["MESSAGE"]; dup {
		t.Error("MESSAGE should not be duplicated into Fields")
	}
}

func TestParseEmptyInput(t *testing.T) {
	events, err := Parse(strings.NewReader("   \n  \n"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestParseMalformedJSONReturnsError(t *testing.T) {
	in := `{"MESSAGE":"ok"}
{this is not json}`
	_, err := Parse(strings.NewReader(in))
	if err == nil {
		t.Fatal("Parse should return an error on malformed JSON")
	}
}

// Parse sets the collection origin on every event it produces.
func TestParseWithOrigin(t *testing.T) {
	events, err := ParseWithOrigin(strings.NewReader(sampleEntry), event.OriginHistorical)
	if err != nil {
		t.Fatalf("ParseWithOrigin returned error: %v", err)
	}
	if events[0].Origin != event.OriginHistorical {
		t.Errorf("Origin = %q, want historical", events[0].Origin)
	}
}

// --- streaming parser ---

// ParseStream must deliver events one by one, in order, without buffering the
// whole stream first.
func TestParseStreamDeliversEachEventInOrder(t *testing.T) {
	in := `{"MESSAGE":"first","__CURSOR":"c1"}
{"MESSAGE":"second","__CURSOR":"c2"}
{"MESSAGE":"third","__CURSOR":"c3"}`

	var got []event.Event
	err := ParseStream(strings.NewReader(in), event.OriginLive, func(e event.Event) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("ParseStream returned error: %v", err)
	}
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Message != w {
			t.Errorf("got[%d].Message = %q, want %q", i, got[i].Message, w)
		}
		if got[i].Origin != event.OriginLive {
			t.Errorf("got[%d].Origin = %q, want live", i, got[i].Origin)
		}
	}
}

// A consumer error must stop the stream immediately and propagate untouched,
// so callers can use sentinel errors to stop early.
func TestParseStreamStopsOnConsumerError(t *testing.T) {
	sentinel := errors.New("stop now")
	in := `{"MESSAGE":"first"}
{"MESSAGE":"second"}`

	var count int
	err := ParseStream(strings.NewReader(in), event.OriginLive, func(e event.Event) error {
		count++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ParseStream error = %v, want the consumer sentinel", err)
	}
	if count != 1 {
		t.Errorf("consumer called %d times, want 1 (stream must stop)", count)
	}
}

func TestParseStreamEmptyInput(t *testing.T) {
	var count int
	err := ParseStream(strings.NewReader(""), event.OriginLive, func(e event.Event) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("ParseStream returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("consumer called %d times, want 0", count)
	}
}

func TestParseStreamMalformedJSONReturnsError(t *testing.T) {
	err := ParseStream(strings.NewReader("{not json}"), event.OriginLive, func(e event.Event) error {
		t.Fatal("consumer must not be called on malformed input")
		return nil
	})
	if err == nil {
		t.Fatal("ParseStream should return an error on malformed JSON")
	}
}
