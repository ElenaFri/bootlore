package summary

import (
	"regexp"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// base is a fixed wall-clock instant used to build deterministic fixtures.
var base = time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)

// sampleBoot is a small, interpreted boot: five events of one boot with mixed
// significance, categories, timestamps and monotonic offsets (deliberately out
// of chronological order), plus one bare event with no timestamps.
func sampleBoot() []event.Event {
	return []event.Event{
		{BootID: "boot-a", RealtimeTimestamp: base, MonotonicTimestamp: 1 * time.Second, Category: event.CategoryKernel, Significance: event.SignificanceNormal},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(2 * time.Second), MonotonicTimestamp: 3 * time.Second, Category: event.CategoryNetwork, Significance: event.SignificanceWarning},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(5 * time.Second), MonotonicTimestamp: 6 * time.Second, Category: event.CategorySystemd, Significance: event.SignificanceFailure},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(4 * time.Second), MonotonicTimestamp: 2 * time.Second, Category: event.CategoryKernel, Significance: event.SignificanceNormal},
		{BootID: "boot-a", Category: event.CategoryUnclassified, Significance: event.SignificanceUnknown},
	}
}

func TestSummarizeCountsAndSignificance(t *testing.T) {
	s := Summarize(sampleBoot())

	if s.Total != 5 {
		t.Errorf("Total = %d, want 5", s.Total)
	}
	if s.Normal != 2 {
		t.Errorf("Normal = %d, want 2", s.Normal)
	}
	if s.Warnings != 1 {
		t.Errorf("Warnings = %d, want 1", s.Warnings)
	}
	if s.Failures != 1 {
		t.Errorf("Failures = %d, want 1", s.Failures)
	}
	if s.Unassessed != 1 {
		t.Errorf("Unassessed = %d, want 1", s.Unassessed)
	}
}

func TestSummarizeCategories(t *testing.T) {
	s := Summarize(sampleBoot())

	want := map[event.Category]int{
		event.CategoryKernel:       2,
		event.CategoryNetwork:      1,
		event.CategorySystemd:      1,
		event.CategoryUnclassified: 1,
	}
	if len(s.Categories) != len(want) {
		t.Fatalf("Categories = %v, want %v", s.Categories, want)
	}
	for cat, n := range want {
		if s.Categories[cat] != n {
			t.Errorf("Categories[%q] = %d, want %d", cat, s.Categories[cat], n)
		}
	}
}

func TestSummarizeTimingAndBoot(t *testing.T) {
	s := Summarize(sampleBoot())

	if !s.First.Equal(base) {
		t.Errorf("First = %v, want %v", s.First, base)
	}
	if want := base.Add(5 * time.Second); !s.Last.Equal(want) {
		t.Errorf("Last = %v, want %v", s.Last, want)
	}
	if s.Span != 5*time.Second {
		t.Errorf("Span = %v, want 5s", s.Span)
	}
	if s.Elapsed != 6*time.Second {
		t.Errorf("Elapsed = %v, want 6s", s.Elapsed)
	}
	if s.BootID != "boot-a" {
		t.Errorf("BootID = %q, want boot-a", s.BootID)
	}
	if s.Boots != 1 {
		t.Errorf("Boots = %d, want 1", s.Boots)
	}
}

func TestSummarizeEmpty(t *testing.T) {
	s := Summarize(nil)

	if s.Total != 0 {
		t.Errorf("Total = %d, want 0", s.Total)
	}
	if len(s.Categories) != 0 {
		t.Errorf("Categories = %v, want empty", s.Categories)
	}
	if !s.First.IsZero() || !s.Last.IsZero() {
		t.Errorf("First/Last = %v/%v, want zero", s.First, s.Last)
	}
	if s.Span != 0 || s.Elapsed != 0 {
		t.Errorf("Span/Elapsed = %v/%v, want 0", s.Span, s.Elapsed)
	}
	if s.BootID != "" || s.Boots != 0 {
		t.Errorf("BootID/Boots = %q/%d, want empty/0", s.BootID, s.Boots)
	}
}

func TestSummarizeMultipleBoots(t *testing.T) {
	events := []event.Event{
		{BootID: "boot-a", RealtimeTimestamp: base, Significance: event.SignificanceNormal},
		{BootID: "boot-b", RealtimeTimestamp: base.Add(time.Second), Significance: event.SignificanceNormal},
	}
	s := Summarize(events)

	if s.Boots != 2 {
		t.Errorf("Boots = %d, want 2", s.Boots)
	}
	// With more than one boot the identifier is ambiguous and left empty.
	if s.BootID != "" {
		t.Errorf("BootID = %q, want empty for multiple boots", s.BootID)
	}
}

func TestFormatReportContents(t *testing.T) {
	out := Format(Summarize(sampleBoot()))

	for _, want := range []string{"Boot summary", "boot-a", "KERNEL", "NETWORK", "SYSTEMD", "(unclassified)", "+6.000s"} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).MatchString(out) {
			t.Errorf("report missing %q\n--- report ---\n%s", want, out)
		}
	}

	// Metric lines carry their counts, regardless of the exact spacing used
	// for alignment.
	for _, re := range []string{
		`Events:\s+5`,
		`Normal:\s+2`,
		`Warnings:\s+1`,
		`Failures:\s+1`,
		`KERNEL\s+2`,
	} {
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("report does not match %q\n--- report ---\n%s", re, out)
		}
	}
}

func TestFormatMultipleBoots(t *testing.T) {
	events := []event.Event{
		{BootID: "boot-a", RealtimeTimestamp: base, Significance: event.SignificanceNormal},
		{BootID: "boot-b", RealtimeTimestamp: base.Add(time.Second), Significance: event.SignificanceNormal},
	}
	out := Format(Summarize(events))

	if !regexp.MustCompile(`multiple: 2 boots`).MatchString(out) {
		t.Errorf("report should flag that it spans multiple boots:\n%s", out)
	}
}

func TestFormatEmptyReport(t *testing.T) {
	out := Format(Summarize(nil))

	if !regexp.MustCompile(`Events:\s+0`).MatchString(out) {
		t.Errorf("empty report should report zero events:\n%s", out)
	}
	// Nothing noteworthy and no timestamps: the report must still be valid,
	// not crash or invent an observation window.
	if regexp.MustCompile(`span`).MatchString(out) && !regexp.MustCompile(`no events`).MatchString(out) {
		t.Errorf("empty report should not show an observation span:\n%s", out)
	}
}

// BootLabel names a single boot, reports when the events span several, and says
// so when none carried an identifier.
func TestBootLabel(t *testing.T) {
	cases := []struct {
		name string
		s    Summary
		want string
	}{
		{"single", Summary{BootID: "boot-a", Boots: 1}, "boot-a"},
		{"multiple", Summary{Boots: 3}, "(multiple: 3 boots)"},
		{"unknown", Summary{Boots: 0}, "(unknown)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BootLabel(tc.s); got != tc.want {
				t.Errorf("BootLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
