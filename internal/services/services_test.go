package services

import (
	"regexp"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// failedBoot has three failures: foo.service fails twice (identified by the
// "unit.type: ..." message prefix), bluetooth.service once (same prefix), and a
// kernel oops with no unit, which is not a service and must be ignored.
func failedBoot() []event.Event {
	return []event.Event{
		{Message: "foo.service: Failed to start Foo.", MonotonicTimestamp: 2 * time.Second, Significance: event.SignificanceFailure},
		{Message: "foo.service: Start request repeated too quickly.", MonotonicTimestamp: 2500 * time.Millisecond, Significance: event.SignificanceFailure},
		{Message: "BUG: kernel NULL pointer dereference", MonotonicTimestamp: 1 * time.Second, Significance: event.SignificanceFailure},
		{Message: "bluetooth.service: Failed with result 'exit-code'.", MonotonicTimestamp: 6 * time.Second, Significance: event.SignificanceFailure},
	}
}

func TestAnalyzeFailedServices(t *testing.T) {
	r := Analyze(failedBoot())

	if len(r.Failed) != 2 {
		t.Fatalf("Failed = %+v, want 2 units", r.Failed)
	}
	// Order is chronological (first failure first): foo at +2s, bluetooth at +6s.
	if r.Failed[0].Unit != "foo.service" {
		t.Errorf("Failed[0].Unit = %q, want foo.service", r.Failed[0].Unit)
	}
	if r.Failed[0].Count != 2 {
		t.Errorf("foo.service Count = %d, want 2", r.Failed[0].Count)
	}
	if r.Failed[0].At != 2*time.Second {
		t.Errorf("foo.service At = %v, want 2s (first failure)", r.Failed[0].At)
	}
	if r.Failed[0].Message != "foo.service: Failed to start Foo." {
		t.Errorf("foo.service Message = %q, want the first failure message", r.Failed[0].Message)
	}
	if r.Failed[1].Unit != "bluetooth.service" || r.Failed[1].Count != 1 {
		t.Errorf("Failed[1] = %+v, want bluetooth.service count 1", r.Failed[1])
	}
}

func TestAnalyzeIgnoresNonServiceFailures(t *testing.T) {
	r := Analyze(failedBoot())
	for _, f := range r.Failed {
		if f.Unit == "" {
			t.Errorf("a failure without a unit was reported: %+v", f)
		}
	}
}

// Units are also identified by the UNIT and USER_UNIT subject fields, which the
// systemd manager sets to say which unit a message is about, when the message
// itself carries no "unit.type: ..." prefix. The emitting/hosting unit
// (_SYSTEMD_UNIT, e.g. user@1000.service) is deliberately NOT used to attribute
// a failure: on a real journal every entry carries one, so blaming it would
// misattribute unrelated crashes to the session host.
func TestAnalyzeIdentifiesUnitFromSubjectFields(t *testing.T) {
	events := []event.Event{
		{Source: "systemd", Fields: map[string]string{"UNIT": "sshd.service"}, Message: "Dependency failed for OpenSSH.", MonotonicTimestamp: time.Second, Significance: event.SignificanceFailure},
		{Source: "systemd", Fields: map[string]string{"USER_UNIT": "xdg-desktop-portal.service"}, Message: "Dependency failed for Portal service.", MonotonicTimestamp: 2 * time.Second, Significance: event.SignificanceFailure},
		{Unit: "user@1000.service", Source: "systemd-coredump", Message: "Process 155721 (qemu) dumped core.", MonotonicTimestamp: 3 * time.Second, Significance: event.SignificanceFailure},
		{Message: "total meltdown", MonotonicTimestamp: 4 * time.Second, Significance: event.SignificanceFailure},
	}
	r := Analyze(events)

	got := map[string]bool{}
	for _, f := range r.Failed {
		got[f.Unit] = true
	}
	for _, want := range []string{"sshd.service", "xdg-desktop-portal.service"} {
		if !got[want] {
			t.Errorf("expected %s among failures, got %+v", want, r.Failed)
		}
	}
	// The coredump (attributable only to the hosting unit) and the unitless
	// "total meltdown" must not be reported as failed services.
	if got["user@1000.service"] {
		t.Errorf("must not blame the hosting unit for a crash: %+v", r.Failed)
	}
	if len(r.Failed) != 2 {
		t.Errorf("Failed = %+v, want exactly 2 units", r.Failed)
	}
}

// timedBoot pairs "Starting X..." with "Started/Finished X." so their startup
// durations can be reconstructed from the monotonic offsets. Orphan has no
// matching "Starting" and must not produce a timing.
func timedBoot() []event.Event {
	return []event.Event{
		{Message: "Starting Network Manager...", MonotonicTimestamp: 1 * time.Second},
		{Message: "Starting Big Service...", MonotonicTimestamp: 1 * time.Second},
		{Message: "Started Network Manager.", MonotonicTimestamp: 3 * time.Second},
		{Message: "Finished Big Service.", MonotonicTimestamp: 5 * time.Second},
		{Message: "Started Orphan.", MonotonicTimestamp: 2 * time.Second},
	}
}

func TestAnalyzeSlowServicesSortedSlowestFirst(t *testing.T) {
	r := Analyze(timedBoot())

	if len(r.Slow) != 2 {
		t.Fatalf("Slow = %+v, want 2 timings", r.Slow)
	}
	if r.Slow[0].Name != "Big Service" || r.Slow[0].Duration != 4*time.Second {
		t.Errorf("Slow[0] = %+v, want Big Service 4s", r.Slow[0])
	}
	if r.Slow[1].Name != "Network Manager" || r.Slow[1].Duration != 2*time.Second {
		t.Errorf("Slow[1] = %+v, want Network Manager 2s", r.Slow[1])
	}
	if r.Slow[0].Started != 5*time.Second {
		t.Errorf("Big Service Started = %v, want 5s", r.Slow[0].Started)
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	r := Analyze(nil)
	if len(r.Failed) != 0 || len(r.Slow) != 0 {
		t.Errorf("empty analysis = %+v, want no failures or timings", r)
	}
}

// A startup span is only reported when both offsets are known and in order. A
// completion that precedes its "Starting", or an empty description, yields no
// timing rather than a negative or bogus one.
func TestAnalyzeSkipsUnmeasurableTimings(t *testing.T) {
	events := []event.Event{
		{Message: "Starting Clock...", MonotonicTimestamp: 5 * time.Second},
		{Message: "Started Clock.", MonotonicTimestamp: 3 * time.Second}, // out of order
		{Message: "Starting ..."}, // empty description
		{Message: "Started ."},    // empty description
	}
	r := Analyze(events)
	if len(r.Slow) != 0 {
		t.Errorf("Slow = %+v, want none (unmeasurable)", r.Slow)
	}
}

func TestFormatReport(t *testing.T) {
	events := append(failedBoot(), timedBoot()...)
	out := Format(Analyze(events))

	for _, want := range []string{
		"Failed services",
		"foo.service",
		"bluetooth.service",
		"Slow services",
		"Big Service",
		"4.000s",
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(out) {
			t.Errorf("report missing %q\n--- report ---\n%s", want, out)
		}
	}
	// The kernel oops must never appear in a services report.
	if regexp.MustCompile(`NULL pointer`).MatchString(out) {
		t.Errorf("services report must not include non-service failures:\n%s", out)
	}
}

func TestFormatNothingToReport(t *testing.T) {
	out := Format(Analyze(nil))

	if !regexp.MustCompile(`[Nn]o failed services`).MatchString(out) {
		t.Errorf("report should state there were no failed services:\n%s", out)
	}
}
