package render

import (
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

func TestLogLineFull(t *testing.T) {
	e := event.Event{
		Message:           "Started Network Manager.",
		RealtimeTimestamp: time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
		Source:            "systemd",
		PID:               1,
		Priority:          event.PriorityInformational,
	}
	want := "2023-11-14T22:13:20Z [info] systemd[1]: Started Network Manager."
	if got := LogLine(e); got != want {
		t.Errorf("LogLine()\n got = %q\nwant = %q", got, want)
	}
}

func TestLogLineWithoutPID(t *testing.T) {
	e := event.Event{
		Message:           "Linux version 6.6.0",
		RealtimeTimestamp: time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
		Source:            "kernel",
		Priority:          event.PriorityNotice,
	}
	want := "2023-11-14T22:13:20Z [notice] kernel: Linux version 6.6.0"
	if got := LogLine(e); got != want {
		t.Errorf("LogLine()\n got = %q\nwant = %q", got, want)
	}
}

// Missing source/timestamp must degrade gracefully with placeholders rather
// than producing a broken line, and the original message is always preserved.
func TestLogLineMissingFields(t *testing.T) {
	e := event.Event{Message: "something happened"}
	want := "- [unknown] -: something happened"
	if got := LogLine(e); got != want {
		t.Errorf("LogLine()\n got = %q\nwant = %q", got, want)
	}
}
