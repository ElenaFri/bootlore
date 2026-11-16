package event

import (
	"testing"
	"time"
)

func TestPriorityString(t *testing.T) {
	cases := map[Priority]string{
		PriorityEmergency:     "emergency",
		PriorityAlert:         "alert",
		PriorityCritical:      "critical",
		PriorityError:         "error",
		PriorityWarning:       "warning",
		PriorityNotice:        "notice",
		PriorityInformational: "info",
		PriorityDebug:         "debug",
		PriorityUnknown:       "unknown",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("Priority(%d).String() = %q, want %q", int8(p), got, want)
		}
	}
}

func TestPriorityStringInvalidFallsBackToUnknown(t *testing.T) {
	if got := Priority(42).String(); got != "unknown" {
		t.Errorf("Priority(42).String() = %q, want %q", got, "unknown")
	}
}

// ParsePriority maps the journal PRIORITY field ("0".."7") onto the model.
func TestParsePriority(t *testing.T) {
	valid := map[string]Priority{
		"0": PriorityEmergency,
		"1": PriorityAlert,
		"2": PriorityCritical,
		"3": PriorityError,
		"4": PriorityWarning,
		"5": PriorityNotice,
		"6": PriorityInformational,
		"7": PriorityDebug,
	}
	for in, want := range valid {
		got, err := ParsePriority(in)
		if err != nil {
			t.Errorf("ParsePriority(%q) returned unexpected error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePriority(%q) = %v, want %v", in, got, want)
		}
		if got.Syslog() != mustAtoi(in) {
			t.Errorf("ParsePriority(%q).Syslog() = %d, want %s", in, got.Syslog(), in)
		}
	}

	for _, in := range []string{"", "8", "-1", "x", "06", " 3"} {
		got, err := ParsePriority(in)
		if err == nil {
			t.Errorf("ParsePriority(%q) = %v, want error", in, got)
		}
		if got != PriorityUnknown {
			t.Errorf("ParsePriority(%q) on error = %v, want PriorityUnknown", in, got)
		}
	}
}

func TestPriorityValidAndSyslog(t *testing.T) {
	for p := PriorityEmergency; p <= PriorityDebug; p++ {
		if !p.Valid() {
			t.Errorf("Priority %q should be valid", p)
		}
		if p.Syslog() < 0 || p.Syslog() > 7 {
			t.Errorf("Priority %q Syslog() = %d, want within 0..7", p, p.Syslog())
		}
	}

	for _, p := range []Priority{PriorityUnknown, PriorityDebug + 1, Priority(127)} {
		if p.Valid() {
			t.Errorf("Priority(%d) should be invalid", int8(p))
		}
	}
	if PriorityUnknown.Syslog() != -1 {
		t.Errorf("PriorityUnknown.Syslog() = %d, want -1", PriorityUnknown.Syslog())
	}
}

// An event must remain valid and displayable even when BootLore adds no
// interpretation: the HUMAN view falls back to the original message.
func TestEventHumanTextFallsBackToMessage(t *testing.T) {
	e := Event{Message: "Started Network Manager."}
	if got := e.HumanText(); got != "Started Network Manager." {
		t.Errorf("HumanText() with no description = %q, want original message", got)
	}

	e.Human = "Network configuration service started."
	if got := e.HumanText(); got != "Network configuration service started." {
		t.Errorf("HumanText() = %q, want the human description", got)
	}
}

// Interpretation must never overwrite source data: adding a human description
// leaves the original Message untouched.
func TestHumanDescriptionDoesNotTouchSource(t *testing.T) {
	const original = "EXT4-fs (sda1): mounted filesystem"
	e := Event{Message: original, Human: "Root filesystem mounted."}
	if e.Message != original {
		t.Errorf("Message = %q, want original %q preserved", e.Message, original)
	}
}

func TestEventIsClassified(t *testing.T) {
	var unclassified Event
	if unclassified.IsClassified() {
		t.Error("zero-value Event should be unclassified")
	}
	if unclassified.Category != CategoryUnclassified {
		t.Errorf("zero-value Category = %q, want unclassified (empty)", unclassified.Category)
	}

	classified := Event{Category: CategoryKernel}
	if !classified.IsClassified() {
		t.Error("Event with a category should report IsClassified() == true")
	}
}

// A zero-value event (no timestamps, no interpretation) must still be usable.
func TestZeroValueEventIsDisplayable(t *testing.T) {
	var e Event
	if e.HumanText() != "" {
		t.Errorf("HumanText() of zero Event = %q, want empty string", e.HumanText())
	}
	if !e.RealtimeTimestamp.Equal(time.Time{}) {
		t.Error("zero Event should have a zero RealtimeTimestamp")
	}
	if e.Priority != PriorityUnknown {
		t.Errorf("zero Event Priority = %v, want PriorityUnknown", e.Priority)
	}
}

func TestSignificanceString(t *testing.T) {
	cases := map[Significance]string{
		SignificanceUnknown: "unknown",
		SignificanceNormal:  "normal",
		SignificanceWarning: "warning",
		SignificanceFailure: "failure",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Significance(%d).String() = %q, want %q", int8(s), got, want)
		}
	}
	if got := Significance(99).String(); got != "unknown" {
		t.Errorf("Significance(99).String() = %q, want unknown", got)
	}
}

func TestSignificanceIsNoteworthy(t *testing.T) {
	for _, s := range []Significance{SignificanceUnknown, SignificanceNormal} {
		if s.IsNoteworthy() {
			t.Errorf("%s should not be noteworthy", s)
		}
	}
	for _, s := range []Significance{SignificanceWarning, SignificanceFailure} {
		if !s.IsNoteworthy() {
			t.Errorf("%s should be noteworthy", s)
		}
	}
}

// Significance is interpretation: a zero-value event has not been assessed, and
// it stays separate from Priority (source data).
func TestZeroValueEventSignificanceUnknown(t *testing.T) {
	var e Event
	if e.Significance != SignificanceUnknown {
		t.Errorf("zero Event Significance = %v, want unknown", e.Significance)
	}
}

func mustAtoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
