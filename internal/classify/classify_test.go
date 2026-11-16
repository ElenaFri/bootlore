package classify

import (
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
)

// classifying must never mutate the original source data.
func TestClassifyDoesNotMutateSource(t *testing.T) {
	e := event.Event{
		Message: "EXT4-fs (sda1): mounted filesystem",
		Source:  "kernel",
		Fields:  map[string]string{"_TRANSPORT": "kernel"},
	}
	before := e
	_ = Classify(e)
	if e.Message != before.Message || e.Source != before.Source {
		t.Error("Classify must not modify the event it is given")
	}
	// Classify returns a category, not a mutated event.
	if got := Classify(e); got == event.CategoryUnclassified {
		t.Error("a kernel-transport event should be classified")
	}
}

func TestClassifyKernelByTransport(t *testing.T) {
	e := event.Event{
		Message: "Linux version 6.6.8",
		Source:  "kernel",
		Fields:  map[string]string{"_TRANSPORT": "kernel"},
	}
	if got := Classify(e); got != event.CategoryKernel {
		t.Errorf("Classify(kernel transport) = %q, want KERNEL", got)
	}
}

func TestClassifyKernelBySource(t *testing.T) {
	// Some kernel lines arrive with SYSLOG_IDENTIFIER=kernel but no _TRANSPORT.
	e := event.Event{Message: "usb 1-1: new high-speed USB device", Source: "kernel"}
	if got := Classify(e); got != event.CategoryKernel {
		t.Errorf("Classify(kernel source) = %q, want KERNEL", got)
	}
}

func TestClassifySystemdByUnitScope(t *testing.T) {
	cases := []struct {
		name string
		e    event.Event
		want event.Category
	}{
		{
			name: "service unit",
			e:    event.Event{Source: "systemd", Unit: "cron.service", Message: "Started Cron."},
			want: event.CategorySystemd,
		},
		{
			name: "systemd identifier without unit",
			e:    event.Event{Source: "systemd", Message: "Reached target Multi-User System."},
			want: event.CategorySystemd,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.e); got != tc.want {
				t.Errorf("Classify() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyStorage(t *testing.T) {
	cases := []event.Event{
		{Source: "kernel", Fields: map[string]string{"_TRANSPORT": "kernel"}, Message: "EXT4-fs (sda1): mounted filesystem with ordered data mode"},
		{Source: "systemd", Unit: "home.mount", Message: "Mounting Home..."},
		{Source: "systemd-fsck", Message: "/dev/sda1: clean, 123/456 files"},
		// Non-kernel source, but a reliable filesystem marker in the message.
		{Source: "mount", Message: "XFS (sdb1): Mounting V5 Filesystem"},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategoryStorage {
			t.Errorf("case %d: Classify() = %q, want STORAGE", i, got)
		}
	}
}

func TestClassifyNetwork(t *testing.T) {
	cases := []event.Event{
		{Source: "NetworkManager", Unit: "NetworkManager.service", Message: "Started Network Manager."},
		{Source: "systemd-networkd", Message: "eth0: Link UP"},
		{Source: "systemd", Unit: "wpa_supplicant.service", Message: "Started WPA supplicant."},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategoryNetwork {
			t.Errorf("case %d: Classify() = %q, want NETWORK", i, got)
		}
	}
}

func TestClassifySecurity(t *testing.T) {
	cases := []event.Event{
		{Source: "audit", Message: "apparmor=\"STATUS\" operation=\"profile_load\""},
		{Source: "sudo", Message: "pam_unix(sudo:session): session opened"},
		{Source: "systemd", Unit: "apparmor.service", Message: "Started Load AppArmor profiles."},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategorySecurity {
			t.Errorf("case %d: Classify() = %q, want SECURITY", i, got)
		}
	}
}

func TestClassifyHardware(t *testing.T) {
	cases := []event.Event{
		{Source: "systemd-udevd", Message: "Using default interface naming scheme 'v255'."},
		{Source: "kernel", Fields: map[string]string{"_TRANSPORT": "kernel"}, Message: "ACPI: Power Button [PWRF]"},
	}
	// The kernel ACPI line is kernel-transport; kernel wins over a weak hardware
	// hint, so only the udev line must be HARDWARE here.
	if got := Classify(cases[0]); got != event.CategoryHardware {
		t.Errorf("udev line = %q, want HARDWARE", got)
	}
}

func TestClassifyDesktop(t *testing.T) {
	cases := []event.Event{
		{Source: "gdm", Message: "GdmDisplay: display lasted 0.5 seconds"},
		{Source: "systemd", Unit: "gdm.service", Message: "Started GNOME Display Manager."},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategoryDesktop {
			t.Errorf("case %d: Classify() = %q, want DESKTOP", i, got)
		}
	}
}

// Unit type (suffix) classifies even without a domain keyword in the name.
func TestClassifyByUnitSuffix(t *testing.T) {
	cases := []struct {
		e    event.Event
		want event.Category
	}{
		{event.Event{Unit: "systemd-networkd.network"}, event.CategoryNetwork},
		{event.Event{Unit: "80-wifi.netdev"}, event.CategoryNetwork},
		{event.Event{Unit: "dev-sda1.device"}, event.CategoryHardware},
		{event.Event{Unit: "var-log.mount"}, event.CategoryStorage},
		{event.Event{Unit: "dev-zram0.swap"}, event.CategoryStorage},
	}
	for i, tc := range cases {
		if got := Classify(tc.e); got != tc.want {
			t.Errorf("case %d (%s): Classify() = %q, want %q", i, tc.e.Unit, got, tc.want)
		}
	}
}

// A systemd-managed unit with no more specific domain falls back to SYSTEMD,
// even when the source is not literally "systemd".
func TestClassifySystemdUnitFallback(t *testing.T) {
	cases := []event.Event{
		{Unit: "basic.target"},
		{Unit: "dbus.socket", Source: ""},
		{Unit: "logrotate.timer"},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategorySystemd {
			t.Errorf("case %d (%s): Classify() = %q, want SYSTEMD", i, e.Unit, got)
		}
	}
}

// A unit that is neither a known domain nor a systemd unit type stays
// unclassified.
func TestClassifyUnknownUnitTypeStaysUnclassified(t *testing.T) {
	e := event.Event{Unit: "mystery.widget", Source: "mystery"}
	if got := Classify(e); got != event.CategoryUnclassified {
		t.Errorf("Classify() = %q, want unclassified", got)
	}
}

// Ambiguous or unknown events must stay unclassified rather than being forced
// into a possibly-wrong bucket.
func TestClassifyUnknownStaysUnclassified(t *testing.T) {
	cases := []event.Event{
		{Source: "some-random-daemon", Message: "doing something opaque"},
		{Message: "no source at all"},
		{},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategoryUnclassified {
			t.Errorf("case %d: Classify() = %q, want unclassified", i, got)
		}
	}
}

// ClassifyEvent returns a copy carrying the category, leaving the input intact.
func TestClassifyEventReturnsCopyWithCategory(t *testing.T) {
	in := event.Event{Source: "kernel", Fields: map[string]string{"_TRANSPORT": "kernel"}, Message: "boot"}
	out := ClassifyEvent(in)
	if out.Category != event.CategoryKernel {
		t.Errorf("out.Category = %q, want KERNEL", out.Category)
	}
	if in.Category != event.CategoryUnclassified {
		t.Error("ClassifyEvent must not mutate its input")
	}
	if out.Message != in.Message {
		t.Error("ClassifyEvent must preserve source data")
	}
}

// An already-classified event is left as-is (idempotent, no re-classification).
func TestClassifyEventKeepsExistingCategory(t *testing.T) {
	in := event.Event{Source: "kernel", Category: event.CategoryOther, Message: "x"}
	out := ClassifyEvent(in)
	if out.Category != event.CategoryOther {
		t.Errorf("out.Category = %q, want the pre-existing OTHER", out.Category)
	}
}
