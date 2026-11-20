// Package timing reconstructs a boot's duration and timing from the journal.
//
// Its primary source is systemd's own summary line, which is authoritative:
//
//	Startup finished in 1.500s (kernel) + 3.500s (userspace) = 5.000s
//
// ParseStartupSummary turns that line into per-phase durations and a total. When
// the journal does not contain it (it may have scrolled out, or the user cannot
// read it), Analyze falls back to the largest monotonic offset observed and
// marks the total as observed rather than reported, so BootLore never presents an
// estimate as a fact.
//
// Analyze also collects boot milestones from "Reached target <name>." events,
// placed at their monotonic offset since boot, which show how the boot
// progressed through its targets. Format renders a stable, monochrome report.
//
// Timing describes the SYSTEM boot only. The per-user systemd manager
// (user@UID.service) emits its own "Startup finished" and "Reached target"
// lines for each session; those belong to the user session, not the machine's
// boot, so Analyze ignores them to avoid mixing a short user-session total with
// system milestones measured since boot.
package timing

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/render"
)

// Phase is one segment of systemd's startup summary ("kernel", "userspace", ...).
type Phase struct {
	Name     string
	Duration time.Duration
}

// Milestone is a boot target that was reached, at its monotonic offset.
type Milestone struct {
	Name string
	At   time.Duration
}

// Timing is the reconstructed timing of a boot.
type Timing struct {
	// Total is the boot duration: systemd's reported total when available,
	// otherwise the largest monotonic offset observed.
	Total time.Duration
	// FromSummary reports whether Total and Phases came from systemd's
	// authoritative startup summary (true) or were merely observed (false).
	FromSummary bool
	// Phases is systemd's per-phase breakdown, empty without a summary.
	Phases []Phase
	// Milestones are the reached targets, in chronological order.
	Milestones []Milestone
}

// Analyze reconstructs timing from interpreted-or-raw events. It only reads the
// message and monotonic offset, so uninterpreted events work too. It does not
// modify the events.
func Analyze(events []event.Event) Timing {
	var t Timing
	var maxOffset time.Duration

	for _, e := range events {
		// The observed-offset fallback honestly reports the latest offset seen
		// across all events, so it is measured before filtering.
		if e.MonotonicTimestamp > maxOffset {
			maxOffset = e.MonotonicTimestamp
		}
		// Only the system manager defines the system boot. Per-user managers
		// emit their own summary and targets for each session; ignore them so a
		// short user-session total never masquerades as the system boot.
		if !fromSystemManager(e) {
			continue
		}
		if !t.FromSummary {
			if phases, total, ok := ParseStartupSummary(e.Message); ok {
				t.Phases = phases
				t.Total = total
				t.FromSummary = true
			}
		}
		if name, ok := targetReached(e.Message); ok {
			t.Milestones = append(t.Milestones, Milestone{Name: name, At: e.MonotonicTimestamp})
		}
	}

	if !t.FromSummary {
		t.Total = maxOffset
	}
	sort.SliceStable(t.Milestones, func(i, j int) bool {
		return t.Milestones[i].At < t.Milestones[j].At
	})
	return t
}

const (
	startupPrefix = "Startup finished in "
	targetPrefix  = "Reached target "
)

// fromSystemManager reports whether e was emitted by the system manager (PID 1,
// UID 0) rather than a per-user systemd manager. It errs toward the system
// manager: an event is treated as a user-session event only when it carries a
// clear per-user signal.
func fromSystemManager(e event.Event) bool {
	if e.Fields["_SYSTEMD_USER_UNIT"] != "" || e.Fields["USER_UNIT"] != "" {
		return false
	}
	if strings.HasPrefix(e.Unit, "user@") {
		return false
	}
	if uid := e.Fields["_UID"]; uid != "" && uid != "0" {
		return false
	}
	return true
}

// ParseStartupSummary parses a systemd "Startup finished in ..." message into its
// per-phase durations and total. It reports false when msg is not such a line or
// carries no parseable total.
func ParseStartupSummary(msg string) (phases []Phase, total time.Duration, ok bool) {
	if !strings.HasPrefix(msg, startupPrefix) {
		return nil, 0, false
	}
	rest := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(msg, startupPrefix), "."))

	left, totalStr, hasTotal := strings.Cut(rest, " = ")
	if !hasTotal {
		// No breakdown: the whole tail is the total.
		if d, dok := parseDuration(rest); dok {
			return nil, d, true
		}
		return nil, 0, false
	}

	total, ok = parseDuration(totalStr)
	if !ok {
		return nil, 0, false
	}
	for _, part := range strings.Split(left, " + ") {
		durStr, name, pok := cutPhase(part)
		if !pok {
			continue
		}
		if d, dok := parseDuration(durStr); dok {
			phases = append(phases, Phase{Name: name, Duration: d})
		}
	}
	return phases, total, true
}

// cutPhase splits a "<duration> (<name>)" phase into its parts.
func cutPhase(part string) (dur, name string, ok bool) {
	part = strings.TrimSpace(part)
	open := strings.LastIndex(part, "(")
	if open < 0 || !strings.HasSuffix(part, ")") {
		return "", "", false
	}
	dur = strings.TrimSpace(part[:open])
	name = part[open+1 : len(part)-1]
	if dur == "" || name == "" {
		return "", "", false
	}
	return dur, name, true
}

// durUnits maps systemd's duration unit suffixes onto their length.
var durUnits = map[string]time.Duration{
	"us":  time.Microsecond,
	"ms":  time.Millisecond,
	"s":   time.Second,
	"min": time.Minute,
	"h":   time.Hour,
	"d":   24 * time.Hour,
	"w":   7 * 24 * time.Hour,
	"y":   365 * 24 * time.Hour,
}

// durToken matches one "<number><unit>" component ("5.678s", "900ms", "2min").
var durToken = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(us|ms|min|h|d|w|y|s)$`)

// parseDuration parses a systemd-formatted duration, which may be several
// space-separated components ("1min 5.678s"). It reports false on any
// unrecognised token.
func parseDuration(s string) (time.Duration, bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0, false
	}
	var total time.Duration
	for _, tok := range fields {
		m := durToken.FindStringSubmatch(tok)
		if m == nil {
			return 0, false
		}
		val, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			return 0, false
		}
		total += time.Duration(val * float64(durUnits[m[2]]))
	}
	return total, true
}

// targetReached returns the target name from a "Reached target <name>." message.
func targetReached(msg string) (string, bool) {
	if !strings.HasPrefix(msg, targetPrefix) {
		return "", false
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(msg, targetPrefix), "."))
	if name == "" {
		return "", false
	}
	return name, true
}

// Format renders a Timing as a stable, monochrome report without a trailing
// newline. With no total and no milestones it says there is nothing to report.
func Format(t Timing) string {
	if t.Total == 0 && len(t.Milestones) == 0 {
		return "No boot timing information available."
	}

	var b strings.Builder
	b.WriteString("Boot timing")

	source := "observed; systemd reported no startup summary"
	if t.FromSummary {
		source = "reported by systemd"
	}
	fmt.Fprintf(&b, "\n  Total: %s (%s)", render.Duration(t.Total), source)

	if len(t.Phases) > 0 {
		b.WriteString("\n  Phases:")
		for _, p := range t.Phases {
			fmt.Fprintf(&b, "\n    %-12s %s", p.Name, render.Duration(p.Duration))
		}
	}

	b.WriteString("\n  Milestones:")
	if len(t.Milestones) == 0 {
		b.WriteString(" (none observed)")
	} else {
		for _, m := range t.Milestones {
			fmt.Fprintf(&b, "\n    %-9s %s", render.FormatOffset(m.At), m.Name)
		}
	}
	return b.String()
}
