package significance

import (
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
)

// The headline behaviour: operational significance is independent of the journal
// priority. Content can escalate a low-priority line to a failure, and routine
// content can keep a high-priority line normal.
func TestSignificanceIsIndependentOfPriority(t *testing.T) {
	// Low (info) priority, but the content reports a failure.
	failure := event.Event{
		Priority: event.PriorityInformational,
		Message:  "bluetooth.service: Failed with result 'exit-code'.",
	}
	if got := Assess(failure); got != event.SignificanceFailure {
		t.Errorf("info-priority failure message = %v, want failure", got)
	}

	// High (warning) priority, but the content is a routine lifecycle event.
	routine := event.Event{
		Priority: event.PriorityWarning,
		Message:  "Reached target Multi-User System.",
	}
	if got := Assess(routine); got != event.SignificanceNormal {
		t.Errorf("warning-priority lifecycle message = %v, want normal", got)
	}
}

func TestAssessFailureByContent(t *testing.T) {
	messages := []string{
		"bluetooth.service: Failed with result 'exit-code'.",
		"Failed to start Network Manager.",
		"Dependency failed for Local File Systems.",
		"snapd.service: Start request repeated too quickly.",
		"Kernel panic - not syncing: VFS: Unable to mount root fs",
		"traps: Xorg[1234] general protection fault",
		"Out of memory: Killed process 4242 (chrome)",
		"EXT4-fs (sda1): I/O error while writing superblock",
	}
	for _, m := range messages {
		e := event.Event{Priority: event.PriorityInformational, Message: m}
		if got := Assess(e); got != event.SignificanceFailure {
			t.Errorf("Assess(%q) = %v, want failure", m, got)
		}
	}
}

func TestAssessNormalByLifecycleContent(t *testing.T) {
	messages := []string{
		"Starting Network Manager...",
		"Started Network Manager.",
		"Stopped target Multi-User System.",
		"Reached target Sound Card.",
		"Mounted /boot.",
		"Listening on D-Bus System Message Bus Socket.",
		"Finished Rebuild Dynamic Linker Cache.",
		"Linux version 6.6.8-arch1-1",
	}
	for _, m := range messages {
		// Even at warning priority, these stay normal.
		e := event.Event{Priority: event.PriorityWarning, Message: m}
		if got := Assess(e); got != event.SignificanceNormal {
			t.Errorf("Assess(%q) = %v, want normal", m, got)
		}
	}
}

func TestAssessWarningByContent(t *testing.T) {
	// Info priority, but the content signals a non-fatal problem.
	messages := []string{
		"dhcp4 (eth0): request timed out",
		"platform regulatory.0: Direct firmware load failed with error -2",
		"this feature is deprecated and will be removed",
		"could not resolve host",
	}
	for _, m := range messages {
		e := event.Event{Priority: event.PriorityInformational, Message: m}
		if got := Assess(e); got != event.SignificanceWarning {
			t.Errorf("Assess(%q) = %v, want warning", m, got)
		}
	}
}

// A bare "failed" that is not a recognised hard failure must not be inflated to
// a failure when it only carries warning priority.
func TestAssessBareFailedAtWarningIsWarning(t *testing.T) {
	e := event.Event{
		Priority: event.PriorityWarning,
		Message:  "ACPI: _OSC failed (AE_NOT_FOUND); disabling ASPM",
	}
	if got := Assess(e); got != event.SignificanceWarning {
		t.Errorf("Assess(ACPI _OSC failed) = %v, want warning", got)
	}
}

// With no telling content, Assess falls back to the journal priority.
func TestAssessFallsBackToPriority(t *testing.T) {
	cases := []struct {
		priority event.Priority
		want     event.Significance
	}{
		{event.PriorityEmergency, event.SignificanceFailure},
		{event.PriorityCritical, event.SignificanceFailure},
		{event.PriorityError, event.SignificanceFailure},
		{event.PriorityWarning, event.SignificanceWarning},
		{event.PriorityNotice, event.SignificanceNormal},
		{event.PriorityInformational, event.SignificanceNormal},
		{event.PriorityDebug, event.SignificanceNormal},
		{event.PriorityUnknown, event.SignificanceNormal},
	}
	for _, tc := range cases {
		e := event.Event{Priority: tc.priority, Message: "an opaque message with no markers"}
		if got := Assess(e); got != tc.want {
			t.Errorf("Assess(priority=%s) = %v, want %v", tc.priority, got, tc.want)
		}
	}
}

// A high journal priority is not de-escalated by weak content: an error-level
// line stays a failure even if it merely contains a soft word like "failed".
func TestAssessHighPriorityNotDeEscalated(t *testing.T) {
	e := event.Event{
		Priority: event.PriorityError,
		Message:  "ACPI: _OSC failed (AE_NOT_FOUND)",
	}
	if got := Assess(e); got != event.SignificanceFailure {
		t.Errorf("Assess(error-priority) = %v, want failure (not de-escalated)", got)
	}
}

func TestAssessEventSetsSignificance(t *testing.T) {
	in := event.Event{Priority: event.PriorityInformational, Message: "Failed to start Foo."}
	out := AssessEvent(in)
	if out.Significance != event.SignificanceFailure {
		t.Errorf("out.Significance = %v, want failure", out.Significance)
	}
	if in.Significance != event.SignificanceUnknown {
		t.Error("AssessEvent must not mutate its input")
	}
	if out.Message != in.Message || out.Priority != in.Priority {
		t.Error("AssessEvent must preserve source data")
	}
}

// AssessEvent is idempotent: an already-assessed event is left untouched.
func TestAssessEventKeepsExistingSignificance(t *testing.T) {
	in := event.Event{
		Priority:     event.PriorityError, // would otherwise be failure
		Significance: event.SignificanceNormal,
		Message:      "x",
	}
	if out := AssessEvent(in); out.Significance != event.SignificanceNormal {
		t.Errorf("out.Significance = %v, want the pre-existing normal", out.Significance)
	}
}
