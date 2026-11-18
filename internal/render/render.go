// Package render turns BootLore events into textual representations for the
// command-line interface. It currently implements three views:
//
//   - LOG: the original system message with timestamp, priority and source.
//   - HUMAN: BootLore's interpreted description.
//   - Timeline: boot-relative offsets ("+1.200s") with either body.
//
// LogLine/HumanLine print a wall-clock timestamp followed by the matching
// *Body function; TimelineLine prints a monotonic offset instead. Original
// messages are always reproduced verbatim in the LOG view.
package render

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// LogLine formats an event as a single LOG line:
//
//	<RFC3339 timestamp> [<priority>] <source>[<pid>]: <message>
//
// Absent fields degrade gracefully: the timestamp and source fall back to "-"
// and the "[pid]" suffix is omitted when the PID is unknown. The original
// message is reproduced verbatim.
func LogLine(e event.Event) string {
	return wallClock(e) + " " + LogBody(e)
}

// HumanLine formats an event as a single HUMAN line:
//
//	<RFC3339 timestamp> [<significance>] <CATEGORY>: <human text>
//
// It renders what BootLore interpreted: the significance (only when assessed),
// the subsystem (only when classified) and the human description, which falls
// back to the original message when no interpretation is available. Absent
// fields are omitted rather than shown as placeholders — the HUMAN view shows
// what is known, never invented facts.
func HumanLine(e event.Event) string {
	body := HumanBody(e)
	if body == "" {
		return wallClock(e)
	}
	return wallClock(e) + " " + body
}

// LogBody is the LOG view without its timestamp: priority, source, pid and the
// verbatim original message.
func LogBody(e event.Event) string {
	var b strings.Builder

	b.WriteString("[")
	b.WriteString(e.Priority.String())
	b.WriteString("] ")

	if e.Source == "" {
		b.WriteString("-")
	} else {
		b.WriteString(e.Source)
	}
	if e.PID != 0 {
		b.WriteString("[")
		b.WriteString(strconv.Itoa(e.PID))
		b.WriteString("]")
	}

	b.WriteString(": ")
	b.WriteString(e.Message)

	return b.String()
}

// HumanBody is the HUMAN view without its timestamp: significance, subsystem
// and human description, each shown only when known.
func HumanBody(e event.Event) string {
	parts := make([]string, 0, 3)
	if e.Significance != event.SignificanceUnknown {
		parts = append(parts, "["+e.Significance.String()+"]")
	}
	if e.IsClassified() {
		parts = append(parts, string(e.Category)+":")
	}
	if text := e.HumanText(); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

// wallClock renders the event's wall-clock timestamp (or "-" when absent).
func wallClock(e event.Event) string {
	if e.RealtimeTimestamp.IsZero() {
		return "-"
	}
	return e.RealtimeTimestamp.UTC().Format(time.RFC3339)
}

// FormatOffset renders a monotonic duration as a boot-relative offset
// ("+1.200s"), or "-" when it is zero (unknown).
func FormatOffset(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("+%.3fs", d.Seconds())
}

// TimelineLine formats an event for the timeline view: the boot-relative
// monotonic offset, then the view body produced by body (LogBody or HumanBody).
//
//	+1.200s [normal] NETWORK: A systemd unit changed state...
//
// Events without a monotonic timestamp show "-" instead of an invented offset.
func TimelineLine(e event.Event, body func(event.Event) string) string {
	b := body(e)
	if b == "" {
		return FormatOffset(e.MonotonicTimestamp)
	}
	return FormatOffset(e.MonotonicTimestamp) + " " + b
}
