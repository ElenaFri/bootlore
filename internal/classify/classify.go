// Package classify assigns a subsystem category to a BootLore event.
//
// Classification relies on explicit, documented signals, in order of
// confidence:
//
//  1. the unit the emitting process runs in (its name and type);
//  2. for messages from the systemd manager itself, the *subject* unit the
//     message is about (the UNIT / USER_UNIT journal fields);
//  3. the originating identifier (SYSLOG_IDENTIFIER / _COMM);
//  4. the kernel transport;
//  5. a few reliable message markers.
//
// A unit that merely *hosts* the emitting process (user@1000.service,
// session-3.scope, ...) is not a subsystem signal: on a real journal every
// entry carries one, so treating it as such classified everything as SYSTEMD.
// Only systemd itself and its components are SYSTEMD.
//
// Events that match no rule stay CategoryUnclassified rather than being forced
// into a possibly-wrong bucket (architecture §4.4). CategoryOther is reserved
// for things BootLore recognises but that belong to none of the named
// subsystems (for example the D-Bus message bus).
//
// Classification never mutates source data: Classify returns a category, and
// ClassifyEvent returns a copy carrying it.
package classify

import (
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
)

// Journal fields naming units.
const (
	fieldUserUnit        = "_SYSTEMD_USER_UNIT" // user unit the emitting process runs in
	fieldSubjectUnit     = "UNIT"               // unit a system-manager message is about
	fieldSubjectUserUnit = "USER_UNIT"          // unit a user-manager message is about
)

// keywordRules maps a substring found in a unit name or source identifier to a
// category, checked in order. The set is intentionally conservative.
var keywordRules = []struct {
	kw  string
	cat event.Category
}{
	{"networkmanager", event.CategoryNetwork},
	{"networkd", event.CategoryNetwork},
	{"wpa_supplicant", event.CategoryNetwork},
	{"wpa-supplicant", event.CategoryNetwork},
	{"iwd", event.CategoryNetwork},
	{"dhcp", event.CategoryNetwork},
	{"wireguard", event.CategoryNetwork},
	{"apparmor", event.CategorySecurity},
	{"selinux", event.CategorySecurity},
	{"audit", event.CategorySecurity},
	{"sudo", event.CategorySecurity},
	{"polkit", event.CategorySecurity},
	{"fsck", event.CategoryStorage},
	{"udisks", event.CategoryStorage},
	{"cryptsetup", event.CategoryStorage},
	{"lvm", event.CategoryStorage},
	{"mdadm", event.CategoryStorage},
	{"gdm", event.CategoryDesktop},
	{"sddm", event.CategoryDesktop},
	{"lightdm", event.CategoryDesktop},
	{"gnome", event.CategoryDesktop},
	{"xfce", event.CategoryDesktop},
	{"xfconf", event.CategoryDesktop},
	{"xdg-desktop-portal", event.CategoryDesktop},
	{"tumbler", event.CategoryDesktop},
	{"dconf", event.CategoryDesktop},
	{"plasma", event.CategoryDesktop},
	{"kwin", event.CategoryDesktop},
	{"gvfs", event.CategoryDesktop},
	{"gcr-", event.CategoryDesktop},
	{"goa-daemon", event.CategoryDesktop},
	{"at-spi", event.CategoryDesktop},
	{"atspi", event.CategoryDesktop},
	{"wireplumber", event.CategoryDesktop},
	{"pipewire", event.CategoryDesktop},
	{"pulseaudio", event.CategoryDesktop},
	{"udev", event.CategoryHardware},
}

// storageMessageMarkers are reliable filesystem/storage markers in a kernel
// message. They are specific enough to override the generic KERNEL category.
var storageMessageMarkers = []string{
	"ext4-fs", "ext3-fs", "ext2-fs", "xfs (", "btrfs", "f2fs",
	"mounted filesystem", "jbd2", "device-mapper", "md/raid",
}

// Classify returns the subsystem category for e, or CategoryUnclassified when no
// reliable rule applies. It does not modify e.
func Classify(e event.Event) event.Category {
	// 1. The unit(s) the emitting process runs in.
	for _, unit := range emittingUnits(e) {
		if c := categoryByUnit(unit); c != event.CategoryUnclassified {
			return c
		}
	}

	// 2. A systemd manager message is *about* another unit; that subject, not
	//    the manager, says which subsystem the event concerns.
	if strings.EqualFold(e.Source, "systemd") {
		for _, unit := range subjectUnits(e) {
			if c := categoryByUnit(unit); c != event.CategoryUnclassified {
				return c
			}
		}
	}

	// 3. The originating identifier.
	src := strings.ToLower(e.Source)
	if src != "" {
		if c := categoryByKeyword(src); c != event.CategoryUnclassified {
			return c
		}
	}

	// 4. Kernel messages: a few are really about storage; otherwise it is a
	//    genuine kernel event. Weaker hints (ACPI, USB enumeration) stay KERNEL.
	if isKernel(e) {
		if matchesStorageMessage(e.Message) {
			return event.CategoryStorage
		}
		return event.CategoryKernel
	}

	// 5. Reliable message markers.
	if matchesStorageMessage(e.Message) {
		return event.CategoryStorage
	}

	// systemd itself and its components are the SYSTEMD subsystem. A hosting
	// unit alone never is (see the package comment).
	if isSystemdComponent(src) {
		return event.CategorySystemd
	}

	// Recognised plumbing that belongs to none of the named subsystems.
	if isOtherPlumbing(src) {
		return event.CategoryOther
	}

	return event.CategoryUnclassified
}

// ClassifyEvent returns a copy of e carrying its category. An event that is
// already classified is returned unchanged (classification is idempotent and
// never overrides an existing category). Source data is always preserved.
func ClassifyEvent(e event.Event) event.Event {
	if e.Category != event.CategoryUnclassified {
		return e
	}
	e.Category = Classify(e)
	return e
}

// emittingUnits lists, lower-cased, the units the emitting process runs in.
func emittingUnits(e event.Event) []string {
	return lowerNonEmpty(e.Unit, e.Fields[fieldUserUnit])
}

// subjectUnits lists, lower-cased, the units a systemd manager message is about.
func subjectUnits(e event.Event) []string {
	return lowerNonEmpty(e.Fields[fieldSubjectUnit], e.Fields[fieldSubjectUserUnit])
}

func lowerNonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, strings.ToLower(v))
		}
	}
	return out
}

func categoryByUnit(unit string) event.Category {
	switch {
	case strings.HasSuffix(unit, ".mount"),
		strings.HasSuffix(unit, ".automount"),
		strings.HasSuffix(unit, ".swap"):
		return event.CategoryStorage
	case strings.HasSuffix(unit, ".network"),
		strings.HasSuffix(unit, ".netdev"):
		return event.CategoryNetwork
	case strings.HasSuffix(unit, ".device"):
		return event.CategoryHardware
	}
	return categoryByKeyword(unit)
}

func categoryByKeyword(s string) event.Category {
	for _, r := range keywordRules {
		if strings.Contains(s, r.kw) {
			return r.cat
		}
	}
	return event.CategoryUnclassified
}

func matchesStorageMessage(msg string) bool {
	m := strings.ToLower(msg)
	for _, marker := range storageMessageMarkers {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

func isKernel(e event.Event) bool {
	if strings.EqualFold(e.Source, "kernel") {
		return true
	}
	return e.Fields != nil && strings.EqualFold(e.Fields["_TRANSPORT"], "kernel")
}

// isSystemdComponent reports whether the (lower-cased) source is systemd or one
// of its helpers: systemd-coredump, systemd-logind, the "(sd-pam)" session
// helper, ...
func isSystemdComponent(src string) bool {
	return src == "systemd" ||
		strings.HasPrefix(src, "systemd-") ||
		strings.HasPrefix(src, "(sd-")
}

// isOtherPlumbing reports whether the (lower-cased) source is recognised system
// plumbing that fits none of the named subsystems: the D-Bus message bus.
func isOtherPlumbing(src string) bool {
	return src == "dbus" || strings.HasPrefix(src, "dbus-")
}
