package compare

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// baselineBoot is an interpreted earlier boot: four events, one warning, one
// failed unit (bluetooth.service) and systemd's startup summary (5.000s total).
func baselineBoot() []event.Event {
	return []event.Event{
		{BootID: "base-01", MonotonicTimestamp: 1 * time.Second, Category: event.CategoryKernel, Significance: event.SignificanceNormal, Message: "Kernel started."},
		{BootID: "base-01", MonotonicTimestamp: 2 * time.Second, Category: event.CategoryNetwork, Significance: event.SignificanceWarning, Message: "Link is flaky."},
		{BootID: "base-01", MonotonicTimestamp: 3 * time.Second, Category: event.CategorySystemd, Significance: event.SignificanceFailure, Message: "bluetooth.service: Failed with result 'exit-code'."},
		{BootID: "base-01", MonotonicTimestamp: 5 * time.Second, Category: event.CategorySystemd, Significance: event.SignificanceNormal, Message: "Startup finished in 1.000s (kernel) + 4.000s (userspace) = 5.000s"},
	}
}

// currentBoot is an interpreted later boot: three events, no warning, a
// different failed unit (cups.service) and a faster startup summary (4.200s).
func currentBoot() []event.Event {
	return []event.Event{
		{BootID: "curr-02", MonotonicTimestamp: 1 * time.Second, Category: event.CategoryKernel, Significance: event.SignificanceNormal, Message: "Kernel started."},
		{BootID: "curr-02", MonotonicTimestamp: 2 * time.Second, Category: event.CategorySystemd, Significance: event.SignificanceFailure, Message: "cups.service: Failed with result 'exit-code'."},
		{BootID: "curr-02", MonotonicTimestamp: 4200 * time.Millisecond, Category: event.CategorySystemd, Significance: event.SignificanceNormal, Message: "Startup finished in 1.000s (kernel) + 3.200s (userspace) = 4.200s"},
	}
}

func TestCompareSummariesAndTiming(t *testing.T) {
	c := Compare(baselineBoot(), currentBoot())

	if c.Baseline.Summary.Total != 4 || c.Current.Summary.Total != 3 {
		t.Errorf("event totals = %d/%d, want 4/3", c.Baseline.Summary.Total, c.Current.Summary.Total)
	}
	if c.Baseline.Timing.Total != 5*time.Second || c.Current.Timing.Total != 4200*time.Millisecond {
		t.Errorf("timing totals = %v/%v, want 5s/4.2s", c.Baseline.Timing.Total, c.Current.Timing.Total)
	}
	if !c.Baseline.Timing.FromSummary || !c.Current.Timing.FromSummary {
		t.Error("both timings should come from systemd's startup summary")
	}
}

func TestCompareServiceDiff(t *testing.T) {
	c := Compare(baselineBoot(), currentBoot())

	assertUnits(t, "Fixed", c.Fixed, []string{"bluetooth.service"})
	assertUnits(t, "Regressed", c.Regressed, []string{"cups.service"})
	assertUnits(t, "StillFailing", c.StillFailing, nil)
}

// A unit that fails in both boots is still failing, not fixed or regressed.
func TestCompareStillFailing(t *testing.T) {
	failing := func(id string) []event.Event {
		return []event.Event{
			{BootID: id, MonotonicTimestamp: time.Second, Significance: event.SignificanceFailure, Message: "nginx.service: Failed with result 'exit-code'."},
		}
	}
	c := Compare(failing("base"), failing("curr"))

	assertUnits(t, "StillFailing", c.StillFailing, []string{"nginx.service"})
	assertUnits(t, "Fixed", c.Fixed, nil)
	assertUnits(t, "Regressed", c.Regressed, nil)
}

func TestFormatReport(t *testing.T) {
	out := Format(Compare(baselineBoot(), currentBoot()))

	for _, re := range []string{
		`Boot comparison`,
		`Boot ID:\s+base-01\s+curr-02`,
		`Duration:\s+5\.000s\s+4\.200s\s+-0\.800s`,
		`faster`,
		`Events:\s+4\s+3\s+-1`,
		`Warnings:\s+1\s+0\s+-1`,
		`Failures:\s+1\s+1\s+0`,
		`fixed:\s+bluetooth\.service`,
		`new:\s+cups\.service`,
		`still failing:\s+\(none\)`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("report does not match %q\n--- report ---\n%s", re, out)
		}
	}
}

// Swapping the two boots flips every delta's sign: the later boot is now the
// slower, busier baseline, exercising the positive-delta rendering and verdict.
func TestFormatPositiveDeltas(t *testing.T) {
	out := Format(Compare(currentBoot(), baselineBoot()))

	for _, re := range []string{
		`Duration:\s+4\.200s\s+5\.000s\s+\+0\.800s`,
		`slower`,
		`Events:\s+3\s+4\s+\+1`,
		`Warnings:\s+0\s+1\s+\+1`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("report does not match %q\n--- report ---\n%s", re, out)
		}
	}
}

// Comparing a boot with itself yields no change: a zero duration delta and the
// "no change" verdict.
func TestFormatNoChange(t *testing.T) {
	out := Format(Compare(baselineBoot(), baselineBoot()))

	if !regexp.MustCompile(`Duration:\s+5\.000s\s+5\.000s\s+0\.000s`).MatchString(out) {
		t.Errorf("identical boots should show a zero duration delta:\n%s", out)
	}
	if !regexp.MustCompile(`no change`).MatchString(out) {
		t.Errorf("identical boots should read 'no change':\n%s", out)
	}
}

// When neither boot carries systemd's startup summary, the duration is the
// largest observed offset and the verdict must say it is observed, not reported.
func TestFormatObservedDuration(t *testing.T) {
	observed := func(id string, last time.Duration) []event.Event {
		return []event.Event{
			{BootID: id, MonotonicTimestamp: last, Significance: event.SignificanceNormal, Message: "Reached target Multi-User System."},
		}
	}
	out := Format(Compare(observed("base", 6*time.Second), observed("curr", 4*time.Second)))

	if !regexp.MustCompile(`Duration:\s+6\.000s\s+4\.000s\s+-2\.000s`).MatchString(out) {
		t.Errorf("observed durations should still be compared:\n%s", out)
	}
	if !regexp.MustCompile(`observed`).MatchString(out) {
		t.Errorf("verdict should flag that the durations are observed:\n%s", out)
	}
}

// A side with no timing at all shows n/a instead of an invented duration, and
// no verdict is drawn.
func TestFormatDurationNotAvailable(t *testing.T) {
	out := Format(Compare(nil, currentBoot()))

	if !regexp.MustCompile(`Duration:\s+n/a`).MatchString(out) {
		t.Errorf("a boot without timing should show n/a:\n%s", out)
	}
	if regexp.MustCompile(`faster|slower`).MatchString(out) {
		t.Errorf("no verdict should be drawn when a duration is missing:\n%s", out)
	}
}

// An empty comparison must render without crashing and without inventing data.
func TestFormatEmpty(t *testing.T) {
	out := Format(Compare(nil, nil))

	if !regexp.MustCompile(`Boot comparison`).MatchString(out) {
		t.Errorf("empty comparison should still render a report:\n%s", out)
	}
	if !regexp.MustCompile(`\(unknown\)`).MatchString(out) {
		t.Errorf("empty comparison should mark both boots unknown:\n%s", out)
	}
	if !regexp.MustCompile(`still failing:\s+\(none\)`).MatchString(out) {
		t.Errorf("empty comparison should show no failures:\n%s", out)
	}
}

// run0/systemd-run transient units (run-p<pid>-i<id>.service) are ephemeral and
// uniquely named per invocation, so they are churn, not service regressions.
// Compare must keep them out of the fixed/new/still-failing diff while still
// counting them per side so the information is not lost.
func TestCompareExcludesTransientUnits(t *testing.T) {
	base := []event.Event{
		{BootID: "base", MonotonicTimestamp: 1 * time.Second, Significance: event.SignificanceFailure, Message: "bluetooth.service: Failed with result 'exit-code'."},
		{BootID: "base", MonotonicTimestamp: 2 * time.Second, Significance: event.SignificanceFailure, Message: "run-p100-i200.service: Failed with result 'exit-code'."},
	}
	curr := []event.Event{
		{BootID: "curr", MonotonicTimestamp: 1 * time.Second, Significance: event.SignificanceFailure, Message: "cups.service: Failed with result 'exit-code'."},
		{BootID: "curr", MonotonicTimestamp: 2 * time.Second, Significance: event.SignificanceFailure, Message: "run-p300-i400.service: Failed with result 'exit-code'."},
		{BootID: "curr", MonotonicTimestamp: 3 * time.Second, Significance: event.SignificanceFailure, Message: "run-p500-i600.service: Failed with result 'exit-code'."},
	}
	c := Compare(base, curr)

	assertUnits(t, "Fixed", c.Fixed, []string{"bluetooth.service"})
	assertUnits(t, "Regressed", c.Regressed, []string{"cups.service"})
	assertUnits(t, "StillFailing", c.StillFailing, nil)
	if c.Baseline.Transient != 1 || c.Current.Transient != 2 {
		t.Errorf("transient counts = %d/%d, want 1/2", c.Baseline.Transient, c.Current.Transient)
	}
}

// Real 32-character boot IDs must be shortened so the comparison's columns stay
// aligned instead of overflowing and running into each other.
func TestFormatShortBootID(t *testing.T) {
	base := []event.Event{{BootID: "7955a8f946284524a943135341482d10", MonotonicTimestamp: time.Second, Significance: event.SignificanceNormal, Message: "x"}}
	curr := []event.Event{{BootID: "4ddb446046c34de6a277b743bb29d2a6", MonotonicTimestamp: time.Second, Significance: event.SignificanceNormal, Message: "y"}}
	out := Format(Compare(base, curr))

	if !strings.Contains(out, "7955a8f94628") || !strings.Contains(out, "4ddb446046c3") {
		t.Errorf("short boot IDs should appear:\n%s", out)
	}
	if strings.Contains(out, "7955a8f946284524a943135341482d10") {
		t.Errorf("full 32-char boot ID should be shortened:\n%s", out)
	}
}

// When transient units were excluded, Format must note how many, per side, so the
// reader knows they existed without seeing every unique throwaway name.
func TestFormatTransientNote(t *testing.T) {
	base := []event.Event{{BootID: "base", MonotonicTimestamp: time.Second, Significance: event.SignificanceFailure, Message: "run-p1-i1.service: Failed with result 'exit-code'."}}
	out := Format(Compare(base, nil))

	if !regexp.MustCompile(`transient:\s+1`).MatchString(out) {
		t.Errorf("report should note the excluded transient units:\n%s", out)
	}
}

// A flat duration delta can hide which phase actually changed. When both boots
// carry systemd's per-phase startup summary, Format must break the duration down
// so a firmware or boot-loader slowdown is never mistaken for a Linux regression.
func TestFormatPhaseBreakdown(t *testing.T) {
	phased := func(id, msg string) []event.Event {
		return []event.Event{{BootID: id, MonotonicTimestamp: time.Second, Significance: event.SignificanceNormal, Message: msg}}
	}
	base := phased("base", "Startup finished in 10.000s (firmware) + 4.000s (loader) + 1.000s (kernel) + 2.000s (initrd) + 3.000s (userspace) = 20.000s")
	curr := phased("curr", "Startup finished in 18.000s (firmware) + 5.000s (loader) + 1.000s (kernel) + 2.000s (initrd) + 2.000s (userspace) = 28.000s")
	out := Format(Compare(base, curr))

	for _, re := range []string{
		`firmware:\s+10\.000s\s+18\.000s\s+\+8\.000s`,
		`loader:\s+4\.000s\s+5\.000s\s+\+1\.000s`,
		`kernel:\s+1\.000s\s+1\.000s\s+0\.000s`,
		`userspace:\s+3\.000s\s+2\.000s\s+-1\.000s`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("phase breakdown does not match %q\n--- report ---\n%s", re, out)
		}
	}
}

// Without systemd's per-phase summary there is nothing to break down: Format must
// not invent phase rows for observed-only durations.
func TestFormatNoPhaseBreakdownWhenObserved(t *testing.T) {
	observed := func(id string, last time.Duration) []event.Event {
		return []event.Event{{BootID: id, MonotonicTimestamp: last, Significance: event.SignificanceNormal, Message: "Reached target Multi-User System."}}
	}
	out := Format(Compare(observed("base", 6*time.Second), observed("curr", 4*time.Second)))

	if regexp.MustCompile(`firmware:|loader:|userspace:`).MatchString(out) {
		t.Errorf("observed-only durations should have no phase breakdown:\n%s", out)
	}
}

// When one boot measured a phase the other did not (a BIOS boot has no firmware
// phase; a boot without a loader has none), the breakdown shows the known side
// and marks the other n/a, never inventing a delta against a missing phase.
func TestFormatPhaseBreakdownAsymmetric(t *testing.T) {
	phased := func(id, msg string) []event.Event {
		return []event.Event{{BootID: id, MonotonicTimestamp: time.Second, Significance: event.SignificanceNormal, Message: msg}}
	}
	base := phased("base", "Startup finished in 2.000s (loader) + 1.000s (kernel) + 3.000s (userspace) = 6.000s")
	curr := phased("curr", "Startup finished in 10.000s (firmware) + 1.000s (kernel) + 3.000s (userspace) = 14.000s")
	out := Format(Compare(base, curr))

	if !regexp.MustCompile(`firmware:\s+n/a\s+10\.000s`).MatchString(out) {
		t.Errorf("a phase measured only in the current boot should show n/a for the baseline:\n%s", out)
	}
	if !regexp.MustCompile(`loader:\s+2\.000s\s+n/a`).MatchString(out) {
		t.Errorf("a phase measured only in the baseline should show n/a for the current:\n%s", out)
	}
}

// assertUnits compares a sorted unit slice against the expected set.
func assertUnits(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
