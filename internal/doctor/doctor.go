// Package doctor checks the local system's compatibility with BootLore and
// the permissions of the invoking user, and turns the raw observations into a
// small set of human-readable checks.
//
// The analysis itself (Analyze) is pure: it works on Findings gathered by the
// CLI layer (journal backend probes), so it stays unit-testable without a
// system journal. Rendering (Render) is monochrome-safe plain text, following
// BootLore's no-colours output rules.
package doctor

import (
	"fmt"
	"strings"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/timing"
)

// ErrBackend is the sentinel reported when the journal backend cannot be
// reached at all. It mirrors collector.ErrBackendUnavailable for callers that
// construct findings by hand.
var ErrBackend = collector.ErrBackendUnavailable

// Status is the outcome of a single doctor check.
type Status int

const (
	// Pass means the check succeeded.
	Pass Status = iota
	// Warn means bootlore works but a feature will be limited.
	Warn
	// Fail means something BootLore relies on is missing or unusable.
	Fail
)

// String renders the status as a short lowercase word, suitable for plain
// monochrome output.
func (s Status) String() string {
	switch s {
	case Pass:
		return "ok"
	case Warn:
		return "warn"
	default:
		return "fail"
	}
}

// Check is one doctor finding: a named aspect of the system, its status, a
// short detail line and, when needed, a hint to fix it.
type Check struct {
	Name   string
	Status Status
	Detail string
	Hint   string
}

// CheckOrder lists the checks in the order Render displays them.
var CheckOrder = []string{
	"journalctl",
	"journal",
	"boot-events",
	"boots",
}

// Findings are the raw observations Analyze works on. Zero values mean
// "not observed": no events read (or none visible) and no boot history.
type Findings struct {
	Events     []event.Event
	BootCount  int
	BackendErr error
}

// Events builds findings from a successful backend read: evts is what the
// current boot yielded and boots is the number of boots the journal recorded.
func Events(evts []event.Event, boots int) Findings {
	return Findings{Events: evts, BootCount: boots}
}

// BackendErr builds findings for a backend that could not be reached at all.
func BackendErr(err error) Findings {
	return Findings{BackendErr: err}
}

// Analyze turns raw findings into the ordered set of compatibility and
// permission checks. It never returns nil: every check in CheckOrder is always
// present, so callers can rely on a stable output shape.
func Analyze(f Findings) []Check {
	if f.BackendErr != nil {
		return []Check{
			{Name: "journalctl", Status: Fail, Detail: "cannot run journalctl", Hint: fmt.Sprintf("%v; install systemd (it provides journalctl), or feed BootLore from a file or stdin instead", f.BackendErr)},
			{Name: "journal", Status: Fail, Detail: "journal unreadable", Hint: "fix journalctl above and re-run bootlore doctor"},
			{Name: "boot-events", Status: Fail, Detail: "no events", Hint: "fix journalctl above and re-run bootlore doctor"},
			{Name: "boots", Status: Fail, Detail: "unknown", Hint: "fix journalctl above and re-run bootlore doctor"},
		}
	}

	checks := []Check{
		{Name: "journalctl", Status: Pass, Detail: "journalctl is available"},
		{Name: "journal", Status: Fail, Detail: "no boot events at all", Hint: "boot from a systemd system and read the journal BootLore can see; if permission is denied, join the systemd-journal group"},
		{Name: "boot-events", Status: Fail, Detail: "no events in the current boot", Hint: "the journal produced nothing for this boot; check journalctl -b by hand"},
	}

	if len(f.Events) > 0 {
		checks[1].Status = Pass
		checks[1].Detail = "the journal yields boot events"
		checks[1].Hint = ""
		checks[2].Status = Pass
		checks[2].Detail = fmt.Sprintf("%d events in the current boot", len(f.Events))
		checks[2].Hint = ""
	}
	if timing.OnlyUserSession(f.Events) {
		checks[1].Status = Fail
		checks[1].Detail = "only your user-session journal is visible"
		checks[1].Hint = "join the systemd-journal group (sudo usermod -aG systemd-journal $USER, then log back in), or pipe the system journal: sudo journalctl -b -o json | bootlore collect"
	}

	boots := Check{Name: "boots"}
	switch n := f.BootCount; {
	case n >= 2:
		boots.Status = Pass
		boots.Detail = fmt.Sprintf("%d boots recorded", n)
	case n == 1:
		boots.Status = Warn
		boots.Detail = "only one boot recorded"
		boots.Hint = "bootlore compare contrasts two boots; reboot once to record a previous boot"
	default:
		boots.Status = Fail
		boots.Detail = "no boot history"
		boots.Hint = "the journal records no past boot; compare needs at least a previous boot to contrast against"
	}
	checks = append(checks, boots)

	return checks
}

// Worst returns the most severe status of a set of checks: any Fail outranks
// Warn, which outranks Pass. The zero value is Pass, so empty input passes.
func Worst(checks []Check) Status {
	worst := Pass
	for _, c := range checks {
		if c.Status > worst {
			worst = c.Status
		}
	}
	return worst
}

// Render formats checks as plain monochrome text, one check per line followed
// by its hint line when present.
func Render(checks []Check) string {
	width := 0
	for _, c := range checks {
		if len(c.Name) > width {
			width = len(c.Name)
		}
	}

	var b strings.Builder
	for _, c := range checks {
		fmt.Fprintf(&b, "%-*s  %-4s  %s\n", width, c.Name, c.Status, strings.TrimSpace(c.Detail))
		if c.Hint != "" {
			fmt.Fprintf(&b, "%*s  hint: %s\n", width, "", c.Hint)
		}
	}
	return b.String()
}
