// Package event defines BootLore's internal event model: the central data
// structure shared by every component (collector, parser, classifier,
// interpreter and the CLI/TUI presentation layers).
//
// The model deliberately separates two kinds of information:
//
//   - Source data: exactly what the system reported (message, timestamps,
//     priority, origin process, journal metadata). It is never modified.
//   - Interpretation: what BootLore adds on top (classification, a concise
//     human description, an educational explanation). It is always optional
//     and must never overwrite source data.
//
// An Event therefore remains valid and displayable even when no classification,
// description or explanation is available: in that case the HUMAN view falls
// back to the original message.
package event

import (
	"fmt"
	"time"
)

// Priority mirrors the syslog severity levels carried by the systemd journal
// PRIORITY field, from Emergency (0) to Debug (7).
//
// The zero value is PriorityUnknown so that an uninitialised Event does not
// accidentally look like an emergency. Use ParsePriority to convert a journal
// PRIORITY string, and Syslog to recover the canonical 0..7 number.
type Priority int8

const (
	// PriorityUnknown means the priority could not be determined. It is the
	// zero value of Priority.
	PriorityUnknown Priority = iota
	PriorityEmergency
	PriorityAlert
	PriorityCritical
	PriorityError
	PriorityWarning
	PriorityNotice
	PriorityInformational
	PriorityDebug
)

var priorityNames = map[Priority]string{
	PriorityUnknown:       "unknown",
	PriorityEmergency:     "emergency",
	PriorityAlert:         "alert",
	PriorityCritical:      "critical",
	PriorityError:         "error",
	PriorityWarning:       "warning",
	PriorityNotice:        "notice",
	PriorityInformational: "info",
	PriorityDebug:         "debug",
}

// String returns a lower-case human-readable name for the priority.
func (p Priority) String() string {
	if name, ok := priorityNames[p]; ok {
		return name
	}
	return "unknown"
}

// Valid reports whether p is one of the eight defined syslog severities.
// PriorityUnknown is not considered valid.
func (p Priority) Valid() bool {
	return p >= PriorityEmergency && p <= PriorityDebug
}

// Syslog returns the canonical syslog number (0 for Emergency .. 7 for Debug),
// or -1 when the priority is unknown/invalid.
func (p Priority) Syslog() int {
	if !p.Valid() {
		return -1
	}
	return int(p - PriorityEmergency)
}

// ParsePriority converts a journal PRIORITY field ("0".."7") into a Priority.
// Any other input returns PriorityUnknown and a non-nil error.
func ParsePriority(s string) (Priority, error) {
	switch s {
	case "0":
		return PriorityEmergency, nil
	case "1":
		return PriorityAlert, nil
	case "2":
		return PriorityCritical, nil
	case "3":
		return PriorityError, nil
	case "4":
		return PriorityWarning, nil
	case "5":
		return PriorityNotice, nil
	case "6":
		return PriorityInformational, nil
	case "7":
		return PriorityDebug, nil
	default:
		return PriorityUnknown, fmt.Errorf("event: invalid priority %q", s)
	}
}

// Category identifies the subsystem an event belongs to. Events that cannot be
// classified reliably stay CategoryUnclassified rather than being forced into a
// possibly-wrong bucket.
type Category string

const (
	// CategoryUnclassified is the zero value: no reliable category assigned.
	CategoryUnclassified Category = ""
	CategoryKernel       Category = "KERNEL"
	CategoryHardware     Category = "HARDWARE"
	CategoryStorage      Category = "STORAGE"
	CategoryNetwork      Category = "NETWORK"
	CategorySystemd      Category = "SYSTEMD"
	CategorySecurity     Category = "SECURITY"
	CategoryDesktop      Category = "DESKTOP"
	CategoryOther        Category = "OTHER"
)

// Significance expresses the operational importance of an event as judged by
// BootLore, deliberately kept separate from the journal Priority.
//
// Priority is what the system reported (source data); Significance is BootLore's
// interpretation. A warning priority does not necessarily mean something failed,
// and a failure may be reported at a low priority (architecture §4.4).
type Significance int8

const (
	// SignificanceUnknown is the zero value: the event has not been assessed.
	SignificanceUnknown Significance = iota
	// SignificanceNormal is routine, expected activity.
	SignificanceNormal
	// SignificanceWarning is something off that is not (yet) a failure.
	SignificanceWarning
	// SignificanceFailure is an operation that did not succeed.
	SignificanceFailure
)

var significanceNames = map[Significance]string{
	SignificanceUnknown: "unknown",
	SignificanceNormal:  "normal",
	SignificanceWarning: "warning",
	SignificanceFailure: "failure",
}

// String returns a lower-case name for the significance level.
func (s Significance) String() string {
	if name, ok := significanceNames[s]; ok {
		return name
	}
	return "unknown"
}

// IsNoteworthy reports whether the event warrants attention: a warning or a
// failure.
func (s Significance) IsNoteworthy() bool {
	return s == SignificanceWarning || s == SignificanceFailure
}

// Origin records how BootLore observed an event: replayed from the journal
// (historical) or followed as it arrived (live).
type Origin string

const (
	// OriginUnknown is the zero value.
	OriginUnknown    Origin = ""
	OriginHistorical Origin = "historical"
	OriginLive       Origin = "live"
)

// Event is the normalized representation of a single boot-related event.
//
// Source data is populated by the collector/parser from the journal.
// Interpretation fields are filled in later by the classifier and interpreter
// and may be empty.
type Event struct {
	// --- Source data: exactly what the system reported. Never modified. ---

	// Message is the original journal message (MESSAGE). It is always preserved.
	Message string
	// RealtimeTimestamp is the wall-clock time (__REALTIME_TIMESTAMP).
	RealtimeTimestamp time.Time
	// MonotonicTimestamp is the time since boot (__MONOTONIC_TIMESTAMP).
	MonotonicTimestamp time.Duration
	// BootID is the boot identifier (_BOOT_ID).
	BootID string
	// Source is the originating identifier (SYSLOG_IDENTIFIER, or _COMM).
	Source string
	// PID is the originating process id (_PID), or 0 when unknown.
	PID int
	// Unit is the systemd unit responsible for the event (_SYSTEMD_UNIT).
	Unit string
	// Priority is the journal severity (PRIORITY).
	Priority Priority
	// Cursor is the opaque journal position (__CURSOR), used to resume reading.
	Cursor string
	// Fields retains any remaining raw journal fields so that nothing from the
	// original record is lost during normalization.
	Fields map[string]string
	// Origin records whether the event was replayed or observed live.
	Origin Origin

	// --- Interpretation: added by BootLore, optional. ---

	// Category is the subsystem classification, or CategoryUnclassified.
	Category Category
	// Significance is BootLore's assessment of operational importance,
	// independent of Priority. The zero value is SignificanceUnknown.
	Significance Significance
	// Human is a concise, faithful reformulation of the event, or empty.
	Human string
	// Explanation is additional educational/technical context, or empty.
	Explanation string
}

// HumanText returns the HUMAN view of the event: the human description when one
// is available, otherwise a faithful fallback to the original message.
func (e Event) HumanText() string {
	if e.Human != "" {
		return e.Human
	}
	return e.Message
}

// IsClassified reports whether a reliable subsystem category has been assigned.
func (e Event) IsClassified() bool {
	return e.Category != CategoryUnclassified
}
