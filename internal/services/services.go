// Package services turns an interpreted boot into a systemd-service diagnosis:
// which services failed, and how long services took to start.
//
// Failures are taken from events BootLore assessed as failures that name a
// systemd unit, from the "unit.type: ..." message prefix or the UNIT / USER_UNIT
// subject fields the systemd manager sets to say which unit a message is about.
// The emitting/hosting unit (_SYSTEMD_UNIT, e.g. user@1000.service) is
// deliberately not used: on a real journal every entry carries one, so blaming
// it would misattribute unrelated crashes to the session host. A failure with no
// identifiable subject unit, such as a kernel oops or a bare core dump, is not a
// service failure and is left out.
//
// Startup timings are reconstructed by pairing a "Starting <description>..."
// event with the matching "Started <description>." or "Finished
// <description>." event and measuring the gap between their monotonic offsets.
// The pairing is by description, exactly as systemd logs it, and only spans with
// both offsets available are reported, so a timing is a measured fact, never a
// guess.
//
// Analyze is pure (it never mutates the events); Format renders a stable,
// monochrome text report for the CLI.
package services

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/render"
)

// Failure is a systemd unit that failed during the boot.
type Failure struct {
	// Unit is the systemd unit that failed.
	Unit string
	// Message is the first failure message observed for the unit.
	Message string
	// At is the monotonic offset of the first failure (0 when unknown).
	At time.Duration
	// Count is the number of failure events observed for the unit.
	Count int
}

// Timing is a reconstructed service startup duration.
type Timing struct {
	// Name is the service description, as systemd logged it.
	Name string
	// Duration is how long the service took to start.
	Duration time.Duration
	// Started is the monotonic offset at which the startup completed.
	Started time.Duration
}

// Report is the result of analysing a boot's services.
type Report struct {
	// Failed lists failed units in order of first failure (chronological).
	Failed []Failure
	// Slow lists reconstructed startup timings, slowest first.
	Slow []Timing
}

// unitSuffixes are the systemd unit types whose names BootLore recognises in a
// message prefix ("bluetooth.service: ...").
var unitSuffixes = []string{
	".service", ".socket", ".mount", ".automount", ".swap",
	".target", ".timer", ".path", ".device", ".scope", ".slice",
}

// Analyze inspects interpreted events and returns the failed services and the
// startup timings it could reconstruct. It does not modify the events.
func Analyze(events []event.Event) Report {
	return Report{
		Failed: failedServices(events),
		Slow:   slowServices(events),
	}
}

// failedServices groups failure events by the unit they name, preserving the
// order in which each unit first failed.
func failedServices(events []event.Event) []Failure {
	index := make(map[string]int) // unit -> position in out
	var out []Failure
	for _, e := range events {
		if e.Significance != event.SignificanceFailure {
			continue
		}
		unit, ok := failedUnit(e)
		if !ok {
			continue
		}
		if i, seen := index[unit]; seen {
			out[i].Count++
			continue
		}
		index[unit] = len(out)
		out = append(out, Failure{
			Unit:    unit,
			Message: e.Message,
			At:      e.MonotonicTimestamp,
			Count:   1,
		})
	}
	return out
}

// failedUnit returns the systemd unit a failure event is about. It prefers the
// unit named in the message prefix ("unit.type: ..."), then the UNIT / USER_UNIT
// subject fields. It never uses the emitting/hosting unit (_SYSTEMD_UNIT), and
// reports false when nothing identifies a subject unit.
func failedUnit(e event.Event) (string, bool) {
	if u := unitFromMessage(e.Message); u != "" {
		return u, true
	}
	for _, field := range []string{"UNIT", "USER_UNIT"} {
		if u := e.Fields[field]; u != "" {
			return u, true
		}
	}
	return "", false
}

// unitFromMessage extracts a unit name from a "unit.type: ..." message prefix,
// or "" when the prefix is not a recognisable unit name.
func unitFromMessage(msg string) string {
	prefix, _, ok := strings.Cut(msg, ": ")
	if !ok || strings.ContainsAny(prefix, " \t") {
		return ""
	}
	for _, suffix := range unitSuffixes {
		if strings.HasSuffix(prefix, suffix) {
			return prefix
		}
	}
	return ""
}

// slowServices reconstructs startup durations by pairing each "Starting X..."
// with the next "Started X." / "Finished X." for the same description, and
// returns them sorted slowest first.
func slowServices(events []event.Event) []Timing {
	starting := make(map[string]time.Duration) // description -> offset of "Starting"

	var out []Timing
	for _, e := range events {
		if desc, ok := describedBy(e.Message, "Starting "); ok {
			starting[desc] = e.MonotonicTimestamp
			continue
		}
		desc, name, ok := completion(e.Message)
		if !ok {
			continue
		}
		startedAt, pending := starting[desc]
		if !pending {
			continue
		}
		delete(starting, desc)
		// Only report a measured span: both offsets known and in order.
		if startedAt == 0 || e.MonotonicTimestamp == 0 || e.MonotonicTimestamp < startedAt {
			continue
		}
		out = append(out, Timing{
			Name:     name,
			Duration: e.MonotonicTimestamp - startedAt,
			Started:  e.MonotonicTimestamp,
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Duration > out[j].Duration
	})
	return out
}

// completion recognises a "Started X." / "Finished X." message and returns the
// pairing key (the normalised description) and the display name.
func completion(msg string) (key, name string, ok bool) {
	for _, verb := range []string{"Started ", "Finished "} {
		if name, ok := describedBy(msg, verb); ok {
			return name, name, true
		}
	}
	return "", "", false
}

// describedBy returns the description part of a "<verb> <description>[.|...]"
// message with trailing dots trimmed, or false when the verb does not match.
func describedBy(msg, verb string) (string, bool) {
	if !strings.HasPrefix(msg, verb) {
		return "", false
	}
	desc := strings.TrimSpace(strings.TrimPrefix(msg, verb))
	desc = strings.TrimRight(desc, ".")
	if desc == "" {
		return "", false
	}
	return desc, true
}

// Format renders a Report as a stable, monochrome text report, without a
// trailing newline.
func Format(r Report) string {
	var b strings.Builder

	if len(r.Failed) == 0 {
		b.WriteString("No failed services.")
	} else {
		b.WriteString("Failed services:")
		for _, f := range r.Failed {
			b.WriteString("\n  ")
			b.WriteString(f.Unit)
			b.WriteString("  (")
			b.WriteString(render.FormatOffset(f.At))
			if f.Count > 1 {
				b.WriteString(pluralFailures(f.Count))
			}
			b.WriteString(")\n    ")
			b.WriteString(f.Message)
		}
	}

	b.WriteString("\n\n")
	if len(r.Slow) == 0 {
		b.WriteString("No service startup times could be reconstructed.")
		return b.String()
	}

	b.WriteString("Slow services (slowest first):")
	for _, t := range r.Slow {
		b.WriteString("\n  ")
		b.WriteString(render.Duration(t.Duration))
		b.WriteString("  ")
		b.WriteString(render.FormatOffset(t.Started))
		b.WriteString("  ")
		b.WriteString(t.Name)
	}
	return b.String()
}

// pluralFailures renders the repeated-failure count (", 3 failures").
func pluralFailures(n int) string {
	return ", " + strconv.Itoa(n) + " failures"
}
