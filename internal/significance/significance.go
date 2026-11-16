// Package significance assesses the operational importance of an event,
// deliberately separate from its journal priority.
//
// The journal PRIORITY says how the system labelled a message; it does not
// reliably say whether something actually went wrong. A unit failure is often
// logged at info priority, while plenty of warning-priority lines are routine.
// Assess therefore looks primarily at message content, using priority only as a
// fallback, and never de-escalates a genuinely high-priority line based on weak
// wording (architecture §4.4).
//
// The rules are explicit and conservative: an event that matches nothing falls
// back to a priority mapping rather than a guess.
package significance

import (
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
)

// failureMarkers are unambiguous, operationally-significant failures. They take
// precedence over everything, including priority.
var failureMarkers = []string{
	"failed with result",
	"failed to start",
	"failed to mount",
	"failed to activate",
	"failed to load",
	"failed to listen",
	"dependency failed",
	"start request repeated too quickly",
	"kernel panic",
	"general protection fault",
	"segfault",
	"oops:",
	"bug: ",
	"out of memory",
	"oom-kill",
	"i/o error",
}

// normalMarkers identify routine lifecycle activity. They keep an event normal
// even when it carries a warning priority (but not emergency..error priority).
var normalMarkers = []string{
	"starting ",
	"started ",
	"stopping ",
	"stopped ",
	"reached target",
	"mounting ",
	"mounted ",
	"unmounted ",
	"listening on",
	"finished ",
	"created slice",
	"activating ",
	"reloading ",
	"linux version",
}

// warningMarkers signal a non-fatal problem worth attention. A bare "failed"
// lives here, not among the hard failures, so soft failures are not inflated.
var warningMarkers = []string{
	"warning",
	"deprecated",
	"timed out",
	"timeout",
	"retrying",
	"degraded",
	"could not",
	"cannot ",
	"unable to",
	"not supported",
	"read-only",
	"refused",
	"failed",
}

// Assess returns the operational significance of e. It does not modify e.
func Assess(e event.Event) event.Significance {
	msg := strings.ToLower(e.Message)
	syslog := e.Priority.Syslog() // -1 when unknown

	// 1. Unambiguous failures in the content win outright.
	if containsAny(msg, failureMarkers) {
		return event.SignificanceFailure
	}
	// 2. A high journal priority (emergency..error) is a strong signal and is
	//    never de-escalated by softer content below.
	if syslog >= 0 && syslog <= 3 {
		return event.SignificanceFailure
	}
	// 3. Routine lifecycle content stays normal despite a warning priority.
	if containsAny(msg, normalMarkers) {
		return event.SignificanceNormal
	}
	// 4. Non-fatal problems in the content.
	if containsAny(msg, warningMarkers) {
		return event.SignificanceWarning
	}
	// 5. Otherwise, fall back to the journal priority.
	if syslog == 4 { // warning
		return event.SignificanceWarning
	}
	return event.SignificanceNormal
}

// AssessEvent returns a copy of e carrying its significance. An event that has
// already been assessed is returned unchanged (idempotent); source data is
// always preserved.
func AssessEvent(e event.Event) event.Event {
	if e.Significance != event.SignificanceUnknown {
		return e
	}
	e.Significance = Assess(e)
	return e
}

func containsAny(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
