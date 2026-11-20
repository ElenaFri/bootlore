package timing

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

func TestParseStartupSummaryTwoPhases(t *testing.T) {
	phases, total, ok := ParseStartupSummary("Startup finished in 1.500s (kernel) + 3.500s (userspace) = 5.000s")
	if !ok {
		t.Fatal("ParseStartupSummary returned ok=false, want true")
	}
	if total != 5*time.Second {
		t.Errorf("total = %v, want 5s", total)
	}
	want := []Phase{{"kernel", 1500 * time.Millisecond}, {"userspace", 3500 * time.Millisecond}}
	if len(phases) != len(want) {
		t.Fatalf("phases = %+v, want %+v", phases, want)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Errorf("phases[%d] = %+v, want %+v", i, phases[i], want[i])
		}
	}
}

// systemd prints longer spans with a "min" component and spaces inside a single
// duration ("2min 0.500s"); the parser must accumulate such multi-token spans.
func TestParseStartupSummaryWithMinutes(t *testing.T) {
	phases, total, ok := ParseStartupSummary("Startup finished in 1min 1.000s (kernel) + 2min 0.500s (userspace) = 3min 1.500s")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if total != 3*time.Minute+1500*time.Millisecond {
		t.Errorf("total = %v, want 3m1.5s", total)
	}
	if phases[0].Duration != time.Minute+time.Second {
		t.Errorf("kernel = %v, want 1m1s", phases[0].Duration)
	}
	if phases[1].Duration != 2*time.Minute+500*time.Millisecond {
		t.Errorf("userspace = %v, want 2m0.5s", phases[1].Duration)
	}
}

func TestParseStartupSummaryFourPhasesAndUnits(t *testing.T) {
	phases, total, ok := ParseStartupSummary("Startup finished in 2.000s (firmware) + 1.000s (loader) + 900ms (kernel) + 4.000s (userspace) = 7.900s")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if total != 7900*time.Millisecond {
		t.Errorf("total = %v, want 7.9s", total)
	}
	names := []string{}
	for _, p := range phases {
		names = append(names, p.Name)
	}
	wantNames := "firmware,loader,kernel,userspace"
	if got := joinNames(names); got != wantNames {
		t.Errorf("phase names = %q, want %q", got, wantNames)
	}
	if phases[2].Duration != 900*time.Millisecond {
		t.Errorf("kernel = %v, want 900ms", phases[2].Duration)
	}
}

// A total with no phase breakdown ("= " absent) still yields the total.
func TestParseStartupSummaryTotalOnly(t *testing.T) {
	phases, total, ok := ParseStartupSummary("Startup finished in 5.000s")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if total != 5*time.Second || len(phases) != 0 {
		t.Errorf("got phases=%+v total=%v, want no phases and 5s", phases, total)
	}
}

// Malformed phases are skipped without discarding the overall total.
func TestParseStartupSummarySkipsUnparseablePhases(t *testing.T) {
	phases, total, ok := ParseStartupSummary("Startup finished in bogus (kernel) + 2.000s + 3.000s (userspace) = 5.000s")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if total != 5*time.Second {
		t.Errorf("total = %v, want 5s", total)
	}
	// "bogus (kernel)" has an unparseable duration; "2.000s" has no "(name)".
	if len(phases) != 1 || phases[0].Name != "userspace" {
		t.Errorf("phases = %+v, want only userspace", phases)
	}
}

// A breakdown with an unreadable total is rejected: BootLore will not infer a
// total from the phases, since a corrupted summary line cannot be trusted.
func TestParseStartupSummaryUnparseableTotal(t *testing.T) {
	if _, _, ok := ParseStartupSummary("Startup finished in 1.500s (kernel) = garbage"); ok {
		t.Error("ok = true, want false for an unreadable total")
	}
}

// Phases whose duration or name is empty are skipped, not emitted as blanks.
func TestParseStartupSummaryEmptyPhaseFields(t *testing.T) {
	phases, total, ok := ParseStartupSummary("Startup finished in (kernel) + 2.000s () + 3.500s (userspace) = 5.000s")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if total != 5*time.Second {
		t.Errorf("total = %v, want 5s", total)
	}
	if len(phases) != 1 || phases[0].Name != "userspace" {
		t.Errorf("phases = %+v, want only userspace", phases)
	}
}

func TestParseStartupSummaryRejectsNonSummary(t *testing.T) {
	for _, msg := range []string{
		"Reached target Multi-User System.",
		"Startup finished in garbage",
		"Startup finished in = 5.000s",
	} {
		if _, _, ok := ParseStartupSummary(msg); ok {
			t.Errorf("ParseStartupSummary(%q) ok = true, want false", msg)
		}
	}
}

// timedBoot is a boot that carries systemd's authoritative startup summary and a
// few "Reached target" milestones, deliberately out of chronological order.
func timedBoot() []event.Event {
	return []event.Event{
		{Message: "Reached target Multi-User System.", MonotonicTimestamp: 4 * time.Second},
		{Message: "Reached target Basic System.", MonotonicTimestamp: 2 * time.Second},
		{Message: "Startup finished in 1.500s (kernel) + 3.500s (userspace) = 5.000s", MonotonicTimestamp: 5 * time.Second},
		{Message: "Reached target Graphical Interface.", MonotonicTimestamp: 4800 * time.Millisecond},
		{Message: "Started Network Manager.", MonotonicTimestamp: 3 * time.Second},
	}
}

// userManagerEvent is an event emitted by the per-user systemd manager
// (user@UID.service), which must never contribute to system boot timing.
func userManagerEvent(msg string, at time.Duration) event.Event {
	return event.Event{
		Message:            msg,
		MonotonicTimestamp: at,
		Unit:               "user@1000.service",
		Fields: map[string]string{
			"_UID":               "1000",
			"_SYSTEMD_USER_UNIT": "init.scope",
			"_SYSTEMD_UNIT":      "user@1000.service",
		},
	}
}

// systemManagerEvent is an event emitted by the system manager (PID 1, UID 0),
// the only source that defines the system boot's timing.
func systemManagerEvent(msg string, at time.Duration) event.Event {
	return event.Event{
		Message:            msg,
		MonotonicTimestamp: at,
		PID:                1,
		Unit:               "init.scope",
	}
}

// The per-user manager also prints "Startup finished in ..." for the user
// session (e.g. "99ms"), but that is not the system boot. Analyze must ignore
// it entirely: no summary, and no user "Reached target" milestones.
func TestAnalyzeIgnoresUserManagerStartupSummary(t *testing.T) {
	events := []event.Event{
		userManagerEvent("Startup finished in 99ms.", 99*time.Millisecond),
		userManagerEvent("Reached target Paths.", 7389*time.Millisecond),
		userManagerEvent("Reached target Bluetooth.", 222290*time.Millisecond),
	}
	ti := Analyze(events)

	if ti.FromSummary {
		t.Error("FromSummary = true, want false (the user manager's summary is not the system boot)")
	}
	if ti.Total != 222290*time.Millisecond {
		t.Errorf("Total = %v, want 222.290s (largest observed offset)", ti.Total)
	}
	if len(ti.Milestones) != 0 {
		t.Errorf("Milestones = %+v, want none (user targets are not system milestones)", ti.Milestones)
	}
}

// When both managers report a startup summary, the system manager's total wins,
// even if the user manager's line appears first in the stream.
func TestAnalyzePrefersSystemManagerSummaryOverUser(t *testing.T) {
	events := []event.Event{
		userManagerEvent("Startup finished in 99ms.", 99*time.Millisecond),
		systemManagerEvent("Startup finished in 1.000s (kernel) + 4.000s (userspace) = 5.000s", 5*time.Second),
	}
	ti := Analyze(events)

	if !ti.FromSummary {
		t.Error("FromSummary = false, want true (the system manager reported a summary)")
	}
	if ti.Total != 5*time.Second {
		t.Errorf("Total = %v, want 5s (system manager), not 99ms (user manager)", ti.Total)
	}
}

// Only the system manager's milestones are kept; user-session targets are
// dropped even when they share a name with a system target.
func TestAnalyzeKeepsOnlySystemMilestones(t *testing.T) {
	events := []event.Event{
		systemManagerEvent("Reached target Basic System.", 3*time.Second),
		userManagerEvent("Reached target Basic System.", 7*time.Second),
		userManagerEvent("Reached target Main User Target.", 8*time.Second),
	}
	ti := Analyze(events)

	want := []Milestone{{"Basic System", 3 * time.Second}}
	if len(ti.Milestones) != len(want) {
		t.Fatalf("Milestones = %+v, want %+v", ti.Milestones, want)
	}
	if ti.Milestones[0] != want[0] {
		t.Errorf("Milestones[0] = %+v, want %+v", ti.Milestones[0], want[0])
	}
}

// fromSystemManager must recognise a system-manager event and reject every
// distinct per-user signal in isolation, so no single journal field can cause a
// user-session event to be counted as the system boot.
func TestFromSystemManager(t *testing.T) {
	cases := []struct {
		name string
		e    event.Event
		want bool
	}{
		{"system manager", systemManagerEvent("x", 0), true},
		{"bare event", event.Event{Message: "x"}, true},
		{"root uid", event.Event{Fields: map[string]string{"_UID": "0"}}, true},
		{"systemd user unit", event.Event{Fields: map[string]string{"_SYSTEMD_USER_UNIT": "app.service"}}, false},
		{"user unit", event.Event{Fields: map[string]string{"USER_UNIT": "app.service"}}, false},
		{"user@ unit", event.Event{Unit: "user@1000.service"}, false},
		{"non-root uid", event.Event{Fields: map[string]string{"_UID": "1000"}}, false},
	}
	for _, c := range cases {
		if got := fromSystemManager(c.e); got != c.want {
			t.Errorf("%s: fromSystemManager = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAnalyzeUsesSystemdSummary(t *testing.T) {
	ti := Analyze(timedBoot())

	if !ti.FromSummary {
		t.Error("FromSummary = false, want true (systemd reported a summary)")
	}
	if ti.Total != 5*time.Second {
		t.Errorf("Total = %v, want 5s", ti.Total)
	}
	if len(ti.Phases) != 2 {
		t.Errorf("Phases = %+v, want 2", ti.Phases)
	}
}

func TestAnalyzeMilestonesChronological(t *testing.T) {
	ti := Analyze(timedBoot())

	want := []Milestone{
		{"Basic System", 2 * time.Second},
		{"Multi-User System", 4 * time.Second},
		{"Graphical Interface", 4800 * time.Millisecond},
	}
	if len(ti.Milestones) != len(want) {
		t.Fatalf("Milestones = %+v, want %+v", ti.Milestones, want)
	}
	for i := range want {
		if ti.Milestones[i] != want[i] {
			t.Errorf("Milestones[%d] = %+v, want %+v", i, ti.Milestones[i], want[i])
		}
	}
}

// Without a systemd summary, the total falls back to the largest monotonic
// offset observed, and this is flagged as observed rather than reported.
func TestAnalyzeFallsBackToObservedOffset(t *testing.T) {
	events := []event.Event{
		{Message: "Started A.", MonotonicTimestamp: 2 * time.Second},
		{Message: "Reached target Basic System.", MonotonicTimestamp: 6 * time.Second},
		{Message: "Started B.", MonotonicTimestamp: 4 * time.Second},
	}
	ti := Analyze(events)

	if ti.FromSummary {
		t.Error("FromSummary = true, want false (no systemd summary)")
	}
	if ti.Total != 6*time.Second {
		t.Errorf("Total = %v, want 6s (largest offset)", ti.Total)
	}
	if len(ti.Phases) != 0 {
		t.Errorf("Phases = %+v, want none without a summary", ti.Phases)
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	ti := Analyze(nil)
	if ti.Total != 0 || ti.FromSummary || len(ti.Milestones) != 0 {
		t.Errorf("empty analysis = %+v, want zero value", ti)
	}
}

func TestFormatReport(t *testing.T) {
	out := Format(Analyze(timedBoot()))

	for _, re := range []string{
		`Boot timing`,
		`Total:\s+5\.000s`,
		`reported by systemd`,
		`kernel\s+1\.500s`,
		`userspace\s+3\.500s`,
		`\+2\.000s\s+Basic System`,
		`\+4\.800s\s+Graphical Interface`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("report does not match %q\n--- report ---\n%s", re, out)
		}
	}
}

func TestFormatObservedAndNoMilestones(t *testing.T) {
	out := Format(Timing{Total: 3 * time.Second, FromSummary: false})

	if !regexp.MustCompile(`observed`).MatchString(out) {
		t.Errorf("observed total should be labelled as such:\n%s", out)
	}
	if !regexp.MustCompile(`(?i)none`).MatchString(out) {
		t.Errorf("report should note that no milestones were observed:\n%s", out)
	}
}

func TestFormatNothing(t *testing.T) {
	out := Format(Analyze(nil))
	if !regexp.MustCompile(`(?i)no boot timing`).MatchString(out) {
		t.Errorf("empty timing should say there is nothing to report:\n%s", out)
	}
}

// cutPhase must reject a "(name)" with no duration and a "<dur> ()" with no
// name, so neither can slip through as a blank phase.
func TestCutPhaseRejectsEmptyFields(t *testing.T) {
	if _, _, ok := cutPhase("(kernel)"); ok {
		t.Error("cutPhase with empty duration ok = true, want false")
	}
	if _, _, ok := cutPhase("5.000s ()"); ok {
		t.Error("cutPhase with empty name ok = true, want false")
	}
}

// parseDuration rejects an empty span and refuses a value that overflows a
// float64, which only a corrupted journal could ever carry.
func TestParseDurationRejectsEmptyAndOverflow(t *testing.T) {
	if _, ok := parseDuration(""); ok {
		t.Error("parseDuration(\"\") ok = true, want false")
	}
	overflow := strings.Repeat("9", 400) + "s"
	if _, ok := parseDuration(overflow); ok {
		t.Error("parseDuration of an overflowing value ok = true, want false")
	}
}

// A "Reached target ." with no name is not a milestone.
func TestTargetReachedRejectsEmptyName(t *testing.T) {
	if _, ok := targetReached("Reached target ."); ok {
		t.Error("targetReached with empty name ok = true, want false")
	}
	ti := Analyze([]event.Event{{Message: "Reached target .", MonotonicTimestamp: time.Second}})
	if len(ti.Milestones) != 0 {
		t.Errorf("Milestones = %+v, want none for a nameless target", ti.Milestones)
	}
}

// joinNames joins phase names with commas for compact assertions.
func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ","
		}
		out += n
	}
	return out
}
