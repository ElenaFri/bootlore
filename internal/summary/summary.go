// Package summary aggregates an interpreted boot into a single high-level
// overview: how many events there were, how many were warnings or failures, how
// they break down by subsystem, and the boot's timing (the observed wall-clock
// window and how far into the boot the events reached).
//
// It operates on events that have already been interpreted (their Category and
// Significance are filled). The aggregation is pure and side-effect free, which
// keeps it easy to test from recorded fixtures; Format turns a Summary into a
// stable, monochrome text report for the CLI.
package summary

import (
	"fmt"
	"strings"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/render"
)

// Summary is a high-level overview of a single interpreted boot.
type Summary struct {
	// BootID is the boot identifier when the events all share one, else empty.
	BootID string
	// Boots counts the distinct non-empty boot identifiers seen. More than one
	// means the events do not describe a single boot.
	Boots int

	// Total is the number of events summarized.
	Total int
	// Normal, Warnings, Failures and Unassessed count events by significance.
	Normal     int
	Warnings   int
	Failures   int
	Unassessed int

	// Categories counts events per subsystem (CategoryUnclassified included).
	Categories map[event.Category]int

	// First and Last are the earliest and latest wall-clock timestamps seen
	// (zero when no event carried a timestamp).
	First time.Time
	Last  time.Time
	// Span is Last minus First: the observed wall-clock window.
	Span time.Duration
	// Elapsed is the largest monotonic offset seen: how far into the boot the
	// last observed event occurred.
	Elapsed time.Duration
}

// Summarize aggregates interpreted events into a Summary. It never modifies the
// events and tolerates missing data: events without a timestamp are ignored for
// the timing figures, and an empty input yields a zero-valued Summary.
func Summarize(events []event.Event) Summary {
	s := Summary{
		Total:      len(events),
		Categories: make(map[event.Category]int),
	}

	bootIDs := make(map[string]bool)
	for _, e := range events {
		switch e.Significance {
		case event.SignificanceFailure:
			s.Failures++
		case event.SignificanceWarning:
			s.Warnings++
		case event.SignificanceNormal:
			s.Normal++
		default:
			s.Unassessed++
		}

		s.Categories[e.Category]++

		if e.BootID != "" {
			bootIDs[e.BootID] = true
		}

		if !e.RealtimeTimestamp.IsZero() {
			if s.First.IsZero() || e.RealtimeTimestamp.Before(s.First) {
				s.First = e.RealtimeTimestamp
			}
			if e.RealtimeTimestamp.After(s.Last) {
				s.Last = e.RealtimeTimestamp
			}
		}
		if e.MonotonicTimestamp > s.Elapsed {
			s.Elapsed = e.MonotonicTimestamp
		}
	}

	s.Boots = len(bootIDs)
	if s.Boots == 1 {
		for id := range bootIDs {
			s.BootID = id
		}
	}
	if !s.First.IsZero() && !s.Last.IsZero() {
		s.Span = s.Last.Sub(s.First)
	}

	return s
}

// categoryOrder is the stable display order for the subsystem breakdown. It
// mirrors the classifier's categories; CategoryUnclassified is shown last.
var categoryOrder = []event.Category{
	event.CategoryKernel,
	event.CategoryHardware,
	event.CategoryStorage,
	event.CategoryNetwork,
	event.CategorySystemd,
	event.CategorySecurity,
	event.CategoryDesktop,
	event.CategoryOther,
	event.CategoryUnclassified,
}

// categoryLabel returns the display label for a category, naming the empty
// (unclassified) category explicitly instead of printing an empty column.
func categoryLabel(c event.Category) string {
	if c == event.CategoryUnclassified {
		return "(unclassified)"
	}
	return string(c)
}

// CategoryLabel is the exported display label for a subsystem category, so other
// renderers (e.g. the report package) label subsystems exactly as the summary
// does.
func CategoryLabel(c event.Category) string {
	return categoryLabel(c)
}

// OrderedCategories returns the subsystems that carry at least one event, in the
// summary's stable display order. It lets other renderers reuse the same
// ordering without duplicating it.
func OrderedCategories(s Summary) []event.Category {
	var out []event.Category
	for _, cat := range categoryOrder {
		if s.Categories[cat] > 0 {
			out = append(out, cat)
		}
	}
	return out
}

// BootLabel returns a display label for a summary's boot identity: the boot ID
// when the events describe a single boot, a count when they span several, or
// "(unknown)" when no event carried an identifier.
func BootLabel(s Summary) string {
	switch {
	case s.Boots > 1:
		return fmt.Sprintf("(multiple: %d boots)", s.Boots)
	case s.BootID == "":
		return "(unknown)"
	default:
		return s.BootID
	}
}

// Format renders a Summary as a stable, monochrome multi-line report, without a
// trailing newline. It degrades gracefully: with no events it says so instead
// of printing an empty or invented observation window.
func Format(s Summary) string {
	var b strings.Builder

	b.WriteString("Boot summary\n")

	fmt.Fprintf(&b, "  %-12s %s\n", "Boot ID:", BootLabel(s))
	fmt.Fprintf(&b, "  %-12s %d\n", "Events:", s.Total)

	if s.Total == 0 {
		b.WriteString("  (no events)")
		return b.String()
	}

	if !s.First.IsZero() {
		fmt.Fprintf(&b, "  %-12s %s to %s (span %s)\n",
			"Observed:",
			s.First.UTC().Format(time.RFC3339),
			s.Last.UTC().Format(time.RFC3339),
			render.Duration(s.Span))
	}
	if s.Elapsed > 0 {
		fmt.Fprintf(&b, "  %-12s %s\n", "Into boot:", render.FormatOffset(s.Elapsed))
	}

	fmt.Fprintf(&b, "  %-12s %d\n", "Normal:", s.Normal)
	fmt.Fprintf(&b, "  %-12s %d\n", "Warnings:", s.Warnings)
	fmt.Fprintf(&b, "  %-12s %d\n", "Failures:", s.Failures)
	if s.Unassessed > 0 {
		fmt.Fprintf(&b, "  %-12s %d\n", "Unassessed:", s.Unassessed)
	}

	b.WriteString("\nBy subsystem:")
	for _, cat := range categoryOrder {
		if n := s.Categories[cat]; n > 0 {
			fmt.Fprintf(&b, "\n  %-16s %d", categoryLabel(cat), n)
		}
	}

	return b.String()
}
