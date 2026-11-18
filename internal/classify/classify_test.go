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

// The unit of the *emitting process* says nothing about the subsystem of an
// event: on a real journal every entry carries one (user@1000.service,
// session-3.scope, ...). A bare systemd unit type therefore must not imply
// SYSTEMD; only systemd itself (the source) does. Found by running the
// classifier on a real journal, where it classified everything as SYSTEMD.
func TestClassifyUnitAloneDoesNotImplySystemd(t *testing.T) {
	cases := []event.Event{
		{Unit: "basic.target"},
		{Unit: "dbus.socket"},
		{Unit: "logrotate.timer"},
		{Unit: "user@1000.service", Source: "some-app"},
		{Unit: "session-3.scope", Source: "qemu-system-x86"},
	}
	for i, e := range cases {
		if got := Classify(e); got != event.CategoryUnclassified {
			t.Errorf("case %d (%s / %s): Classify() = %q, want unclassified", i, e.Unit, e.Source, got)
		}
	}
}

// systemd itself and its components are the SYSTEMD subsystem.
func TestClassifySystemdComponents(t *testing.T) {
	for _, src := range []string{"systemd", "systemd-coredump", "systemd-logind", "systemd-journald", "(sd-pam)"} {
		e := event.Event{Source: src, Unit: "user@1000.service"}
		if got := Classify(e); got != event.CategorySystemd {
			t.Errorf("source %q: Classify() = %q, want SYSTEMD", src, got)
		}
	}
}

// systemd's own job messages carry the unit they are about in UNIT (system
// manager) or USER_UNIT (user manager); that subject, not the emitting unit,
// decides the subsystem.
func TestClassifyUsesSubjectUnit(t *testing.T) {
	cases := []struct {
		name string
		e    event.Event
		want event.Category
	}{
		{
			"system manager, network subject",
			event.Event{Source: "systemd", Unit: "init.scope", Fields: map[string]string{"UNIT": "NetworkManager.service"}, Message: "Started Network Manager."},
			event.CategoryNetwork,
		},
		{
			"system manager, storage subject",
			event.Event{Source: "systemd", Unit: "init.scope", Fields: map[string]string{"UNIT": "home.mount"}, Message: "Mounted /home."},
			event.CategoryStorage,
		},
		{
			"user manager, audio session subject",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"_SYSTEMD_USER_UNIT": "init.scope", "USER_UNIT": "wireplumber.service"}, Message: "Started Multimedia Service Session Manager."},
			event.CategoryDesktop,
		},
		{
			"user manager, dbus-activated desktop service",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "dbus-:1.2-org.xfce.ScreenSaver@0.service"}, Message: "Starting Xfce Screensaver..."},
			event.CategoryDesktop,
		},
		{
			"user manager, gnome and accessibility subjects",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "dbus-:1.9-org.a11y.atspi.Registry@0.service"}, Message: "Started Accessibility services."},
			event.CategoryDesktop,
		},
		{
			"user manager, desktop portal (the bulk of a real session journal)",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "xdg-desktop-portal.service"}, Message: "Dependency failed for Portal service."},
			event.CategoryDesktop,
		},
		{
			"user manager, settings and thumbnail services",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "xfconfd.service"}, Message: "Started Xfce configuration service."},
			event.CategoryDesktop,
		},
		{
			"user manager, thumbnailer",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "tumblerd.service"}, Message: "Started Thumbnailing service."},
			event.CategoryDesktop,
		},
		{
			"user manager, dconf settings backend",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "dconf.service"}, Message: "Started User preferences database."},
			event.CategoryDesktop,
		},
		{
			"user manager, tmpfiles is systemd plumbing with no domain subject",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"USER_UNIT": "systemd-tmpfiles-clean.service"}, Message: "Finished Cleanup of User's Temporary Files and Directories."},
			event.CategorySystemd,
		},
		{
			"user manager, generic milestone has no domain subject",
			event.Event{Source: "systemd", Unit: "user@1000.service", Fields: map[string]string{"_SYSTEMD_USER_UNIT": "init.scope"}, Message: "Reached target Paths."},
			event.CategorySystemd,
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

// Event shapes taken from a real user-session journal: every line carries the
// user manager as emitting unit, so the source identifier and the user unit
// must do the work.
func TestClassifyRealUserSessionSources(t *testing.T) {
	user := func(source, userUnit string) event.Event {
		e := event.Event{Source: source, Unit: "user@1000.service", Fields: map[string]string{}}
		if userUnit != "" {
			e.Fields["_SYSTEMD_USER_UNIT"] = userUnit
		}
		return e
	}
	cases := []struct {
		e    event.Event
		want event.Category
	}{
		{user("xfce4-screensaver-dialog", "dbus-:1.2-org.xfce.ScreenSaver@0.service"), event.CategoryDesktop},
		{user("xfce4-notifyd", ""), event.CategoryDesktop},
		{user("gcr-prompter", "dbus-:1.2-org.gnome.keyring.SystemPrompter@0.service"), event.CategoryDesktop},
		{user("gnome-keyring-daemon", ""), event.CategoryDesktop},
		{user("at-spi2-registryd", "dbus-:1.9-org.a11y.atspi.Registry@0.service"), event.CategoryDesktop},
		{user("wireplumber", "wireplumber.service"), event.CategoryDesktop},
		{user("gvfsd", ""), event.CategoryDesktop},
		{user("dbus-broker-launch", "dbus-broker.service"), event.CategoryOther},
		{user("dbus-broker", "dbus-broker.service"), event.CategoryOther},
		// Recognised only by what we can state reliably: an application we know
		// nothing about must stay unclassified, not inherit the user manager.
		{user("qemu-system-x86", ""), event.CategoryUnclassified},
		{user("obexd", ""), event.CategoryUnclassified},
	}
	for _, tc := range cases {
		if got := Classify(tc.e); got != tc.want {
			t.Errorf("source %q: Classify() = %q, want %q", tc.e.Source, got, tc.want)
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
