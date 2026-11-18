// Package knowledge holds BootLore's educational knowledge base: contextual
// explanations associated with common boot messages and concepts
// (architecture §4.6).
//
// Each entry offers a concise Summary (the basis of the HUMAN view) and three
// levels of explanation (the EXPLAIN view): an accessible Overview, a Technical
// description of the mechanism, and an In-Depth treatment for readers who want
// the full picture. Entries are plain data, independently maintainable and
// validated by tests.
//
// Explanations must distinguish established technical facts from possible
// interpretations and diagnostic hypotheses. The knowledge base is local and
// requires no network access.
package knowledge

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
)

// Level selects the depth of an explanation. The zero value, LevelOverview, is
// the most accessible level.
type Level int8

const (
	LevelOverview  Level = iota // plain language: what it means practically
	LevelTechnical              // the mechanism: how it works
	LevelInDepth                // full educational context
)

var levelNames = map[Level]string{
	LevelOverview:  "overview",
	LevelTechnical: "technical",
	LevelInDepth:   "in-depth",
}

// String returns a lower-case name for the level.
func (l Level) String() string {
	if name, ok := levelNames[l]; ok {
		return name
	}
	return "unknown"
}

// ParseLevel converts a user-facing level name into a Level. Valid names are
// "overview", "technical" and "in-depth" (case-sensitive).
func ParseLevel(s string) (Level, error) {
	switch s {
	case "overview":
		return LevelOverview, nil
	case "technical":
		return LevelTechnical, nil
	case "in-depth":
		return LevelInDepth, nil
	default:
		return LevelOverview, fmt.Errorf("knowledge: unknown level %q (want overview, technical or in-depth)", s)
	}
}

// Entry is one knowledge base entry about a boot concept.
type Entry struct {
	// ID is the stable identifier of the entry.
	ID string
	// Summary is a concise, faithful human description (HUMAN view).
	Summary string
	// Overview is the plain-language explanation.
	Overview string
	// Technical describes how the underlying mechanism works.
	Technical string
	// InDepth provides deeper educational context.
	InDepth string
}

// Text returns the entry's explanation at the given level, or "" for an
// unknown level.
func (e Entry) Text(level Level) string {
	switch level {
	case LevelOverview:
		return e.Overview
	case LevelTechnical:
		return e.Technical
	case LevelInDepth:
		return e.InDepth
	default:
		return ""
	}
}

// entries is the knowledge base, keyed by entry ID.
var entries = map[string]Entry{
	"kernel-version": {
		ID:      "kernel-version",
		Summary: "The Linux kernel is starting and reports its version.",
		Overview: "This is one of the very first boot messages: the kernel — the core of the " +
			"operating system — has taken control from the bootloader and introduces itself. " +
			"Everything else in the boot process happens on top of it.",
		Technical: "The bootloader loaded the kernel image into memory and jumped to its entry " +
			"point. The kernel sets up memory management, CPUs and basic subsystems, then prints " +
			"its version string into the kernel ring buffer, from which journald later collects it.",
		InDepth: "Kernel messages emitted before journald starts are only visible if the ring " +
			"buffer is read before it wraps around, so the earliest lines can be missing or " +
			"truncated. The version string also records who built the kernel and with which " +
			"compiler, and kernel parameters chosen by the bootloader (visible in /proc/cmdline) " +
			"influence much of what follows.",
	},
	"systemd-start": {
		ID:      "systemd-start",
		Summary: "systemd, the init system (PID 1), starts and prepares to launch all other services.",
		Overview: "Once the kernel has finished its own setup it starts exactly one userspace " +
			"program: systemd. From this point systemd is responsible for bringing up — and " +
			"keeping track of — every service on the system.",
		Technical: "The kernel mounts the real root filesystem and executes the init binary as " +
			"PID 1. systemd reads its unit configuration, builds the dependency graph and queues " +
			"the start jobs needed to reach the default target.",
		InDepth: "systemd is a service manager rather than a list of startup scripts: it tracks " +
			"processes in cgroups, supervises them, restarts units according to their Restart= " +
			"policy and starts units in parallel wherever dependencies allow. This message names " +
			"the systemd version and confirms it booted in system mode rather than as a user " +
			"instance.",
	},
	"unit-lifecycle": {
		ID:      "unit-lifecycle",
		Summary: "A systemd unit changed state as part of the boot sequence.",
		Overview: "systemd starts the system piece by piece. This line records one of those " +
			"pieces — a service or similar unit — changing state, in the order its dependencies " +
			"require.",
		Technical: "systemd executes start jobs in dependency order. 'Starting' marks the " +
			"beginning of a job; 'Started' confirms the unit reached its active state " +
			"successfully — for a service, that its processes are running or its one-shot work " +
			"completed.",
		InDepth: "What counts as 'started' depends on the unit's Type=: simple units count as " +
			"started right after the process is forked, forking when the parent exits, and " +
			"notify when the service says so. Jobs that hang are cut off by the configured " +
			"timeout. The full story of a unit is available with systemctl status.",
	},
	"target-reached": {
		ID:      "target-reached",
		Summary: "A boot milestone was reached: all units required for this stage are active.",
		Overview: "A target is a named milestone. Reaching it means everything systemd considers " +
			"required for that stage of the boot has finished starting.",
		Technical: "Targets are units that group other units via Wants= and Requires=. " +
			"'Reached target' is emitted once every start job pulled in by the target has " +
			"completed successfully.",
		InDepth: "Typical chains are sysinit → basic → multi-user → graphical (the default " +
			"target). A target is a synchronization point rather than a process. " +
			"systemctl list-dependencies shows the graph, and systemd-analyze critical-chain " +
			"reveals which units slowed the path to the default target.",
	},
	"filesystem-mounted": {
		ID:      "filesystem-mounted",
		Summary: "A filesystem was made available at its mount point.",
		Overview: "Storage becomes usable once its filesystem is mounted — attached at a " +
			"directory. During boot this includes system locations such as /boot or /home.",
		Technical: "The kernel performed the mount on systemd's behalf, either through a mount " +
			"unit generated from /etc/fstab or through the unit's own configuration. Where the " +
			"filesystem has a journal, it is replayed during the mount.",
		InDepth: "The root filesystem is mounted by the initramfs before systemd runs; " +
			"everything else is normally handled by generated .mount units. The message names " +
			"the device and filesystem type. Errors at this stage usually mean /etc/fstab and " +
			"reality disagree — check with findmnt or systemctl status on the matching mount " +
			"unit.",
	},
	"service-failed": {
		ID:      "service-failed",
		Summary: "A unit reported a failure. The cause is not stated here and needs investigation.",
		Overview: "Something did not start or exited with an error. Boot usually continues, but " +
			"the feature this unit provides may be missing or degraded.",
		Technical: "systemd recorded a failure result for this unit (for example exit-code, " +
			"timeout or a failed dependency). This is a fact about the unit's state, not an " +
			"explanation of why it failed.",
		InDepth: "The established fact is that the unit is in a failed state. Possible causes — " +
			"which remain a hypothesis until checked — include a crashing process, invalid " +
			"configuration or an unmet dependency. Investigate with systemctl status <unit> and " +
			"journalctl -u <unit>; the failure result narrows the search: exit-code points at the " +
			"exit status or signal, timeout at a hang, dependency at the units it required.",
	},
}

// matchFunc decides whether an event matches a rule. msg is the lower-cased
// message, precomputed for convenience.
type matchFunc func(e event.Event, msg string) bool

// rule associates an event pattern with a knowledge entry. Rules are checked in
// order, most specific first.
type rule struct {
	entryID string
	match   matchFunc
}

// rules maps events onto knowledge entries using explicit, documented signals
// (message patterns combined with the source/unit fields).
var rules = []rule{
	{
		entryID: "service-failed",
		match: and(
			fromSystemd,
			containsAny("failed with result", "failed to start"),
		),
	},
	{
		entryID: "filesystem-mounted",
		match: or(
			containsAny("mounted filesystem"),
			and(fromSystemd, prefixAny("mounted ", "mounting ")),
		),
	},
	{
		entryID: "target-reached",
		match:   and(fromSystemd, prefixAny("reached target")),
	},
	{
		entryID: "kernel-version",
		match:   containsAny("linux version"),
	},
	{
		entryID: "systemd-start",
		match:   and(fromSystemd, containsAny("running in system mode")),
	},
	{
		entryID: "unit-lifecycle",
		match: and(
			fromSystemd,
			prefixAny("starting ", "started ", "stopping ", "stopped ", "finished "),
		),
	},
}

// For returns the knowledge entry matching e, or false when no rule applies.
// Callers fall back to the original message when no entry exists.
func For(e event.Event) (Entry, bool) {
	msg := strings.ToLower(e.Message)
	for _, r := range rules {
		if r.match(e, msg) {
			return entries[r.entryID], true
		}
	}
	return Entry{}, false
}

// All returns every entry, sorted by ID for stable iteration.
func All() []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// HasEntryFor reports whether a knowledge entry matches the event. It is a
// convenience for callers (like the TUI) that need to know whether an
// explanation exists without retrieving it.
func HasEntryFor(e event.Event) bool {
	_, ok := For(e)
	return ok
}

// fromSystemd reports whether the event was emitted by the systemd manager
// itself. Merely running inside a systemd unit is not enough: on a real journal
// every process's messages carry the unit that hosts it (user@1000.service,
// session-3.scope, ...), so an application's "Starting..." must not be read as a
// systemd unit lifecycle event.
func fromSystemd(e event.Event, _ string) bool {
	return strings.EqualFold(e.Source, "systemd")
}

func containsAny(patterns ...string) matchFunc {
	return func(_ event.Event, msg string) bool {
		for _, p := range patterns {
			if strings.Contains(msg, p) {
				return true
			}
		}
		return false
	}
}

func prefixAny(patterns ...string) matchFunc {
	return func(_ event.Event, msg string) bool {
		for _, p := range patterns {
			if strings.HasPrefix(msg, p) {
				return true
			}
		}
		return false
	}
}

func and(fs ...matchFunc) matchFunc {
	return func(e event.Event, msg string) bool {
		for _, f := range fs {
			if !f(e, msg) {
				return false
			}
		}
		return true
	}
}

func or(fs ...matchFunc) matchFunc {
	return func(e event.Event, msg string) bool {
		for _, f := range fs {
			if f(e, msg) {
				return true
			}
		}
		return false
	}
}
