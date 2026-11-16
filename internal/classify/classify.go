// Package classify assigns a subsystem category to a BootLore event.
//
// Classification relies on explicit, documented signals — in order of
// confidence: the systemd unit (name and type), the originating identifier
// (SYSLOG_IDENTIFIER / _COMM), the kernel transport, and only as a last resort
// a few reliable message markers. Events that match no rule stay
// CategoryUnclassified rather than being forced into a possibly-wrong bucket
// (architecture §4.4).
//
// Classification never mutates source data: Classify returns a category, and
// ClassifyEvent returns a copy carrying it.
package classify

import (
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
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
	{"gnome-session", event.CategoryDesktop},
	{"plasma", event.CategoryDesktop},
	{"udev", event.CategoryHardware},
}

// storageMessageMarkers are reliable filesystem/storage markers in a kernel
// message. They are specific enough to override the generic KERNEL category.
var storageMessageMarkers = []string{
	"ext4-fs", "ext3-fs", "ext2-fs", "xfs (", "btrfs", "f2fs",
	"mounted filesystem", "jbd2", "device-mapper", "md/raid",
}

// systemdUnitSuffixes identify a systemd-managed unit that carries no more
// specific domain (storage/network/... units are handled before these).
var systemdUnitSuffixes = []string{
	".service", ".target", ".scope", ".slice", ".socket", ".timer", ".path",
}

// Classify returns the subsystem category for e, or CategoryUnclassified when no
// reliable rule applies. It does not modify e.
func Classify(e event.Event) event.Category {
	if unit := strings.ToLower(e.Unit); unit != "" {
		if c := categoryByUnit(unit); c != event.CategoryUnclassified {
			return c
		}
	}

	if src := strings.ToLower(e.Source); src != "" {
		if c := categoryByKeyword(src); c != event.CategoryUnclassified {
			return c
		}
	}

	if isKernel(e) {
		// A few kernel messages are really about storage; otherwise it is a
		// genuine kernel event. Weaker hints (ACPI, USB enumeration) stay KERNEL.
		if matchesStorageMessage(e.Message) {
			return event.CategoryStorage
		}
		return event.CategoryKernel
	}

	if matchesStorageMessage(e.Message) {
		return event.CategoryStorage
	}

	if strings.EqualFold(e.Source, "systemd") || isSystemdUnit(strings.ToLower(e.Unit)) {
		return event.CategorySystemd
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

func isSystemdUnit(unit string) bool {
	for _, suffix := range systemdUnitSuffixes {
		if strings.HasSuffix(unit, suffix) {
			return true
		}
	}
	return false
}
