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

// --- HUMAN view ---

// The HUMAN view shows the interpreted event: what it means, at what
// significance, in which subsystem — falling back to the original message when
// no interpretation exists.
func TestHumanLineFull(t *testing.T) {
	e := event.Event{
		Message:           "Started Network Manager.",
		Human:             "A systemd unit changed state as part of the boot sequence.",
		RealtimeTimestamp: time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
		Category:          event.CategoryNetwork,
		Significance:      event.SignificanceNormal,
	}
	want := "2023-11-14T22:13:20Z [normal] NETWORK: A systemd unit changed state as part of the boot sequence."
	if got := HumanLine(e); got != want {
		t.Errorf("HumanLine()\n got = %q\nwant = %q", got, want)
	}
}

func TestHumanLineFailureSignificance(t *testing.T) {
	e := event.Event{
		Message:      "bluetooth.service: Failed with result 'exit-code'.",
		Human:        "A unit reported a failure.",
		Category:     event.CategorySystemd,
		Significance: event.SignificanceFailure,
	}
	want := "- [failure] SYSTEMD: A unit reported a failure."
	if got := HumanLine(e); got != want {
		t.Errorf("HumanLine()\n got = %q\nwant = %q", got, want)
	}
}

// Without interpretation, the HUMAN view degrades: no invented category, no
// significance bracket, and the original message as text.
func TestHumanLineUninterpretedFallsBack(t *testing.T) {
	e := event.Event{
		Message:           "doing something opaque",
		RealtimeTimestamp: time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
		Significance:      event.SignificanceNormal,
	}
	want := "2023-11-14T22:13:20Z [normal] doing something opaque"
	if got := HumanLine(e); got != want {
		t.Errorf("HumanLine()\n got = %q\nwant = %q", got, want)
	}
}

// Unknown significance is omitted rather than shown as [unknown].
func TestHumanLineUnknownSignificanceOmitted(t *testing.T) {
	e := event.Event{
		Message:           "x",
		RealtimeTimestamp: time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
	}
	want := "2023-11-14T22:13:20Z x"
	if got := HumanLine(e); got != want {
		t.Errorf("HumanLine()\n got = %q\nwant = %q", got, want)
	}
}

func TestHumanLineZeroEvent(t *testing.T) {
	var e event.Event
	if got := HumanLine(e); got != "-" {
		t.Errorf("HumanLine(zero) = %q, want %q", got, "-")
	}
}

// --- timeline view ---

func TestFormatOffset(t *testing.T) {
	cases := map[string]struct {
		d    time.Duration
		want string
	}{
		"zero":             {0, "-"},
		"milliseconds":     {1200 * time.Millisecond, "+1.200s"},
		"sub-second":       {250 * time.Millisecond, "+0.250s"},
		"over a minute":    {95500 * time.Millisecond, "+95.500s"},
		"fractional micro": {1204567 * time.Microsecond, "+1.205s"},
	}
	for _, tc := range cases {
		if got := FormatOffset(tc.d); got != tc.want {
			t.Errorf("FormatOffset(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestTimelineLineHumanBody(t *testing.T) {
	e := event.Event{
		Message:            "Started Network Manager.",
		Human:              "A systemd unit changed state as part of the boot sequence.",
		MonotonicTimestamp: 1200 * time.Millisecond,
		Category:           event.CategoryNetwork,
		Significance:       event.SignificanceNormal,
	}
	want := "+1.200s [normal] NETWORK: A systemd unit changed state as part of the boot sequence."
	if got := TimelineLine(e, HumanBody); got != want {
		t.Errorf("TimelineLine()\n got = %q\nwant = %q", got, want)
	}
}

func TestTimelineLineLogBody(t *testing.T) {
	e := event.Event{
		Message:            "Started Network Manager.",
		MonotonicTimestamp: 1200 * time.Millisecond,
		Source:             "systemd",
		PID:                1,
		Priority:           event.PriorityInformational,
	}
	want := "+1.200s [info] systemd[1]: Started Network Manager."
	if got := TimelineLine(e, LogBody); got != want {
		t.Errorf("TimelineLine()\n got = %q\nwant = %q", got, want)
	}
}

// Without a monotonic timestamp the offset degrades to "-", and an empty body
// yields just the offset — never invented data, never stray spaces.
func TestTimelineLineDegraded(t *testing.T) {
	withBody := event.Event{Category: event.CategoryKernel, Significance: event.SignificanceNormal, Message: "x"}
	if got := TimelineLine(withBody, HumanBody); got != "- [normal] KERNEL: x" {
		t.Errorf("TimelineLine without monotonic = %q", got)
	}
	var zero event.Event
	if got := TimelineLine(zero, HumanBody); got != "-" {
		t.Errorf("TimelineLine(zero) = %q, want %q", got, "-")
	}
}
