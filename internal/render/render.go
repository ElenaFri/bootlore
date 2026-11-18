// Package render turns BootLore events into textual representations for the
// command-line interface. It currently implements the LOG view, which shows the
// original system message together with its timestamp, priority and source,
// without altering the message itself.
package render

import (
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
	var b strings.Builder

	if e.RealtimeTimestamp.IsZero() {
		b.WriteString("-")
	} else {
		b.WriteString(e.RealtimeTimestamp.UTC().Format(time.RFC3339))
	}

	b.WriteString(" [")
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

// HumanLine formats an event as a single HUMAN view line:
//
//	<RFC3339 timestamp> [<significance>] <CATEGORY>: <human text>
//
// It renders what BootLore interpreted: the significance (only when assessed),
// the subsystem (only when classified) and the human description, which falls
// back to the original message when no interpretation is available. Absent
// fields are omitted rather than shown as placeholders — the HUMAN view shows
// what is known, never invented facts.
func HumanLine(e event.Event) string {
	parts := make([]string, 0, 4)
	if e.RealtimeTimestamp.IsZero() {
		parts = append(parts, "-")
	} else {
		parts = append(parts, e.RealtimeTimestamp.UTC().Format(time.RFC3339))
	}
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
