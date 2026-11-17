package interpret_test

import (
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/interpret"
)

// Interpret is the pipeline capstone: one call fills every interpretation field
// (category, significance, human description, explanation) while preserving the
// source data and leaving the input untouched.
func TestInterpretFillsAllInterpretationFields(t *testing.T) {
	in := event.Event{
		Source:   "systemd",
		Unit:     "NetworkManager.service",
		Priority: event.PriorityInformational,
		Message:  "Started Network Manager.",
	}
	out := interpret.Interpret(in)

	if out.Category != event.CategoryNetwork {
		t.Errorf("Category = %q, want NETWORK", out.Category)
	}
	if out.Significance != event.SignificanceNormal {
		t.Errorf("Significance = %v, want normal", out.Significance)
	}
	if out.Human == "" {
		t.Error("Human should be filled from the knowledge base summary")
	}
	if out.Explanation == "" {
		t.Error("Explanation should be filled from the knowledge base")
	}

	// Source data is never modified.
	if out.Message != in.Message || out.Source != in.Source || out.Priority != in.Priority {
		t.Errorf("source data was modified: %+v", out)
	}
	// The input event is not mutated.
	if in.Category != event.CategoryUnclassified || in.Human != "" || in.Explanation != "" {
		t.Errorf("input event was mutated: %+v", in)
	}
}

// A failure event must be classified, marked as a failure, and explained with an
// entry that does not invent a cause.
func TestInterpretFailureEvent(t *testing.T) {
	in := event.Event{
		Source:   "systemd",
		Unit:     "bluetooth.service",
		Priority: event.PriorityInformational,
		Message:  "bluetooth.service: Failed with result 'exit-code'.",
	}
	out := interpret.Interpret(in)

	if out.Category != event.CategorySystemd {
		t.Errorf("Category = %q, want SYSTEMD", out.Category)
	}
	if out.Significance != event.SignificanceFailure {
		t.Errorf("Significance = %v, want failure", out.Significance)
	}
	if out.Human == "" || out.Explanation == "" {
		t.Errorf("failure event should carry a description and explanation: %+v", out)
	}
}

// An event with no reliable interpretation remains valid and displayable: the
// HUMAN view falls back to the original message.
func TestInterpretUnmatchedEventFallsBack(t *testing.T) {
	in := event.Event{
		Source:   "some-daemon",
		Priority: event.PriorityDebug,
		Message:  "doing something opaque",
	}
	out := interpret.Interpret(in)

	if out.Category != event.CategoryUnclassified {
		t.Errorf("Category = %q, want unclassified", out.Category)
	}
	if out.Significance != event.SignificanceNormal {
		t.Errorf("Significance = %v, want normal (debug priority)", out.Significance)
	}
	if out.Human != "" {
		t.Errorf("Human = %q, want empty (no interpretation available)", out.Human)
	}
	if out.Explanation != "" {
		t.Errorf("Explanation = %q, want empty", out.Explanation)
	}
	if got := out.HumanText(); got != "doing something opaque" {
		t.Errorf("HumanText() = %q, want fallback to original message", got)
	}
}

// Interpretation never overwrites what is already there: pre-set interpretation
// fields are preserved (idempotence, like ClassifyEvent and AssessEvent).
func TestInterpretKeepsExistingInterpretation(t *testing.T) {
	in := event.Event{
		Source:       "systemd",
		Unit:         "NetworkManager.service",
		Priority:     event.PriorityInformational,
		Message:      "Started Network Manager.",
		Category:     event.CategoryOther,
		Significance: event.SignificanceWarning,
		Human:        "pre-existing description",
		Explanation:  "pre-existing explanation",
	}
	out := interpret.Interpret(in)
	if out.Category != event.CategoryOther {
		t.Errorf("Category = %q, want the pre-existing OTHER", out.Category)
	}
	if out.Significance != event.SignificanceWarning {
		t.Errorf("Significance = %v, want the pre-existing warning", out.Significance)
	}
	if out.Human != "pre-existing description" {
		t.Errorf("Human = %q, want the pre-existing description", out.Human)
	}
	if out.Explanation != "pre-existing explanation" {
		t.Errorf("Explanation = %q, want the pre-existing explanation", out.Explanation)
	}
}

// A kernel line is classified KERNEL, explained, and matched even though it
// carries no unit.
func TestInterpretKernelLine(t *testing.T) {
	in := event.Event{
		Source:   "kernel",
		Priority: event.PriorityNotice,
		Message:  "Linux version 6.6.8-arch1-1 (linux@archlinux) #1 SMP PREEMPT_DYNAMIC",
	}
	out := interpret.Interpret(in)
	if out.Category != event.CategoryKernel {
		t.Errorf("Category = %q, want KERNEL", out.Category)
	}
	if out.Human == "" || out.Explanation == "" {
		t.Errorf("kernel line should be explained: %+v", out)
	}
}
