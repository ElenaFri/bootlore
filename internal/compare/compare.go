// Package compare contrasts two boot sessions: a baseline (earlier) boot and a
// current (later) one. It is BootLore's answer to "how does this boot compare to
// the previous one?" — did it get faster or slower, did failures appear or
// disappear, did the event mix change.
//
// Compare reuses BootLore's existing per-boot analyses (summary, timing and
// services) so the figures it contrasts are exactly those the individual
// commands report. It draws one honest verdict only where it can: a duration
// delta is labelled faster/slower, and flagged "observed" when either side's
// total came from the largest observed offset rather than systemd's authoritative
// startup summary. When both boots carry that summary, the duration is broken down
// per phase (firmware, loader, kernel, initrd, userspace) so a firmware or
// boot-loader slowdown is never mistaken for a Linux regression. Failed units are
// diffed into those that were fixed, those that newly regressed, and those still
// failing; transient run-* units (systemd-run/run0) are uniquely named per
// invocation, so they are excluded from that diff and merely counted per side.
//
// Both Compare and Format are pure and side-effect free; Format renders a stable,
// monochrome text report for the CLI.
package compare

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/render"
	"github.com/ElenaFri/bootlore/internal/services"
	"github.com/ElenaFri/bootlore/internal/summary"
	"github.com/ElenaFri/bootlore/internal/timing"
)

// Side is one boot's analysis within a comparison.
type Side struct {
	// Summary is the boot's aggregate overview (counts, significance, timing).
	Summary summary.Summary
	// Timing is the boot's reconstructed duration and milestones.
	Timing timing.Timing
	// Failed lists the units that failed during the boot, sorted by name.
	Failed []string
	// Transient counts the run-* transient units that failed during the boot.
	// They are excluded from the Failed set (and the diff) because they are
	// uniquely named per invocation, so they are churn rather than regressions.
	Transient int
}

// Comparison is the difference between a baseline (earlier) boot and a current
// (later) one.
type Comparison struct {
	Baseline Side
	Current  Side

	// Fixed lists units that failed in the baseline but not the current boot.
	Fixed []string
	// Regressed lists units that failed in the current boot but not the baseline.
	Regressed []string
	// StillFailing lists units that failed in both boots.
	StillFailing []string
}

// Compare analyses both boots and diffs them. Each side's events must already be
// interpreted (Category and Significance filled), exactly as summary, timing and
// services expect. It never modifies the events.
func Compare(baseline, current []event.Event) Comparison {
	b := analyse(baseline)
	c := analyse(current)

	inBaseline := unitSet(b.Failed)
	inCurrent := unitSet(c.Failed)

	cmp := Comparison{Baseline: b, Current: c}
	for _, u := range b.Failed {
		if inCurrent[u] {
			cmp.StillFailing = append(cmp.StillFailing, u)
		} else {
			cmp.Fixed = append(cmp.Fixed, u)
		}
	}
	for _, u := range c.Failed {
		if !inBaseline[u] {
			cmp.Regressed = append(cmp.Regressed, u)
		}
	}
	return cmp
}

// analyse runs the per-boot analyses and extracts the sorted set of failed units,
// setting transient run-* units aside so they do not pollute the diff.
func analyse(events []event.Event) Side {
	var failed []string
	var transient int
	for _, f := range services.Analyze(events).Failed {
		if isTransientUnit(f.Unit) {
			transient++
			continue
		}
		failed = append(failed, f.Unit)
	}
	sort.Strings(failed)
	return Side{
		Summary:   summary.Summarize(events),
		Timing:    timing.Analyze(events),
		Failed:    failed,
		Transient: transient,
	}
}

// isTransientUnit reports whether a unit is a run-* transient unit, as created by
// systemd-run and run0. Such units are named uniquely per invocation, so a failed
// one is never the "same" failure across boots.
func isTransientUnit(name string) bool {
	return strings.HasPrefix(name, "run-")
}

// unitSet turns a slice of unit names into a membership set.
func unitSet(units []string) map[string]bool {
	m := make(map[string]bool, len(units))
	for _, u := range units {
		m[u] = true
	}
	return m
}

// Format renders a Comparison as a stable, monochrome text report, without a
// trailing newline.
func Format(c Comparison) string {
	var b strings.Builder
	b.WriteString("Boot comparison\n")
	b.WriteString("  baseline = earlier boot, current = later boot\n\n")

	row := func(label, baseline, current, change string) {
		fmt.Fprintf(&b, "  %-14s %-20s %-20s %s\n", label, baseline, current, change)
	}
	row("", "baseline", "current", "change")
	row("Boot ID:", bootLabel(c.Baseline.Summary), bootLabel(c.Current.Summary), "")

	bt, ct := c.Baseline.Timing.Total, c.Current.Timing.Total
	if bt > 0 && ct > 0 {
		delta := ct - bt
		row("Duration:", render.Duration(bt), render.Duration(ct), signedDuration(delta)+"  ("+durationVerdict(c, delta)+")")
	} else {
		row("Duration:", durationOrNA(bt), durationOrNA(ct), "")
	}
	writePhaseBreakdown(&b, c)

	row("Events:", strconv.Itoa(c.Baseline.Summary.Total), strconv.Itoa(c.Current.Summary.Total),
		signedInt(c.Current.Summary.Total-c.Baseline.Summary.Total))
	row("Warnings:", strconv.Itoa(c.Baseline.Summary.Warnings), strconv.Itoa(c.Current.Summary.Warnings),
		signedInt(c.Current.Summary.Warnings-c.Baseline.Summary.Warnings))
	row("Failures:", strconv.Itoa(c.Baseline.Summary.Failures), strconv.Itoa(c.Current.Summary.Failures),
		signedInt(c.Current.Summary.Failures-c.Baseline.Summary.Failures))

	// The counts above cover each boot's whole session journal, which runs
	// until the next boot (or now). Showing the observed span makes that
	// explicit, and a caveat fires when the two windows differ enough that
	// comparing raw counts would mislead.
	bs, cs := c.Baseline.Summary.Span, c.Current.Summary.Span
	if bs > 0 || cs > 0 {
		row("Session:", spanLabel(bs), spanLabel(cs), "")
	}
	if spansIncomparable(bs, cs) {
		fmt.Fprintf(&b, "  note: the counts above cover each whole session (not just the boot),\n")
		fmt.Fprintf(&b, "        so with spans this different they are not directly comparable.\n")
	}

	b.WriteString("\nService failures:\n")
	writeUnits(&b, "fixed", c.Fixed)
	writeUnits(&b, "new", c.Regressed)
	writeUnits(&b, "still failing", c.StillFailing)
	if c.Baseline.Transient > 0 || c.Current.Transient > 0 {
		fmt.Fprintf(&b, "  %-14s %d baseline, %d current (run-* transient units, excluded)\n",
			"transient:", c.Baseline.Transient, c.Current.Transient)
	}

	return strings.TrimRight(b.String(), "\n")
}

// shortBootID is the number of leading characters of a 32-character boot ID kept
// in the comparison, enough to identify a boot while fitting the fixed columns.
const shortBootID = 12

// spanLabel renders a session span coarsely (days, hours, minutes or seconds),
// so a three-day session reads as "3d 0h" rather than "261013.235s". A zero
// span, i.e. no observed window, reads as n/a.
func spanLabel(d time.Duration) string {
	if d <= 0 {
		return "n/a"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", d/(24*time.Hour), (d%(24*time.Hour))/time.Hour)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", d/time.Hour, (d%time.Hour)/time.Minute)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", d/time.Minute, (d%time.Minute)/time.Second)
	default:
		return fmt.Sprintf("%ds", d/time.Second)
	}
}

// spanIncomparableFactor is how many times longer one session may run than the
// other before their raw event counts stop being directly comparable.
const spanIncomparableFactor = 2

// spansIncomparable reports whether two session spans differ enough that
// comparing their raw counts would mislead. It needs both windows to be known
// (a missing span cannot be judged).
func spansIncomparable(a, b time.Duration) bool {
	if a <= 0 || b <= 0 {
		return false
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return hi > time.Duration(spanIncomparableFactor)*lo
}

// bootLabel renders a boot's identifier for the comparison, shortening real
// 32-character boot IDs so the columns stay aligned. It falls back to
// summary.BootLabel for the "(unknown)" and "(multiple)" cases.
func bootLabel(s summary.Summary) string {
	if s.BootID == "" {
		return summary.BootLabel(s)
	}
	if len(s.BootID) > shortBootID {
		return s.BootID[:shortBootID]
	}
	return s.BootID
}

// phaseOrder is systemd's canonical startup-phase order. The breakdown lists
// phases in this order so a boot that omits some (firmware and loader are absent
// on BIOS systems, or when systemd cannot measure them) still reads naturally.
var phaseOrder = []string{"firmware", "loader", "kernel", "initrd", "userspace"}

// phaseMap indexes a timing's phases by name for per-phase lookup.
func phaseMap(t timing.Timing) map[string]time.Duration {
	m := make(map[string]time.Duration, len(t.Phases))
	for _, p := range t.Phases {
		m[p.Name] = p.Duration
	}
	return m
}

// orderedPhases returns the union of phase names present in either boot, in
// systemd's canonical order, with any unrecognised phases appended in order of
// first appearance so nothing systemd reports is ever dropped.
func orderedPhases(baseline, current timing.Timing) []string {
	present := make(map[string]bool)
	for _, p := range baseline.Phases {
		present[p.Name] = true
	}
	for _, p := range current.Phases {
		present[p.Name] = true
	}

	var names []string
	seen := make(map[string]bool)
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, n := range phaseOrder {
		if present[n] {
			add(n)
		}
	}
	for _, p := range baseline.Phases {
		add(p.Name)
	}
	for _, p := range current.Phases {
		add(p.Name)
	}
	return names
}

// writePhaseBreakdown renders systemd's per-phase durations as indented sub-rows
// of the Duration line, so a flat total delta never hides that a slowdown was in
// firmware or the boot loader rather than in Linux userspace. A phase measured on
// only one side shows "n/a" for the other and draws no delta. It renders nothing
// when neither boot carries a per-phase breakdown.
func writePhaseBreakdown(b *strings.Builder, c Comparison) {
	bp := phaseMap(c.Baseline.Timing)
	cp := phaseMap(c.Current.Timing)
	for _, n := range orderedPhases(c.Baseline.Timing, c.Current.Timing) {
		bd, hasB := bp[n]
		cd, hasC := cp[n]
		baseline, current, change := "n/a", "n/a", ""
		if hasB {
			baseline = render.Duration(bd)
		}
		if hasC {
			current = render.Duration(cd)
		}
		if hasB && hasC {
			change = signedDuration(cd - bd)
		}
		fmt.Fprintf(b, "    %-12s %-20s %-20s %s\n", n+":", baseline, current, change)
	}
}

// durationVerdict describes a duration delta, flagging it as observed when either
// side's total did not come from systemd's authoritative startup summary.
func durationVerdict(c Comparison, delta time.Duration) string {
	var verdict string
	switch {
	case delta < 0:
		verdict = "faster"
	case delta > 0:
		verdict = "slower"
	default:
		verdict = "no change"
	}
	if !(c.Baseline.Timing.FromSummary && c.Current.Timing.FromSummary) {
		verdict += ", observed"
	}
	return verdict
}

// durationOrNA renders a duration, or "n/a" when it is zero (unknown).
func durationOrNA(d time.Duration) string {
	if d == 0 {
		return "n/a"
	}
	return render.Duration(d)
}

// signedInt renders an integer delta with an explicit sign ("+2", "-1", "0").
func signedInt(d int) string {
	if d > 0 {
		return "+" + strconv.Itoa(d)
	}
	return strconv.Itoa(d)
}

// signedDuration renders a duration delta with an explicit sign ("+0.300s",
// "-0.800s", "0.000s").
func signedDuration(d time.Duration) string {
	switch {
	case d > 0:
		return "+" + render.Duration(d)
	case d < 0:
		return "-" + render.Duration(-d)
	default:
		return render.Duration(0)
	}
}

// writeUnits renders one line of the service-failure diff, naming the units or
// "(none)" when the set is empty.
func writeUnits(b *strings.Builder, label string, units []string) {
	value := "(none)"
	if len(units) > 0 {
		value = strings.Join(units, ", ")
	}
	fmt.Fprintf(b, "  %-14s %s\n", label+":", value)
}
