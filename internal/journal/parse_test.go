package journal

import (
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
