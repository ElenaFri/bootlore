//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/vt"
)

// The real probes run unprivileged against the live system: existence is an
// open of /dev/ttyN, occupancy is the /proc scan and the active console is the
// sysfs file. On a system without VTs (container, serial console) the probes
// degrade to "unknown" and every assertion skips.
func TestRealProbesAgainstLiveSystem(t *testing.T) {
	p := vt.NewProbe()

	anyExists := false
	for n := 1; n <= 63; n++ {
		if p.VTExists(n) {
			anyExists = true
			break
		}
	}
	if !anyExists {
		t.Skip("no virtual terminals on this system (container or serial console?)")
	}

	// The active console, when known, is a legal VT number whose device node
	// exists and is reported busy: something (kernel console, getty, Xorg)
	// always holds the foreground terminal.
	if active, ok := p.VTActive(); ok {
		if active < 1 || active > 63 {
			t.Errorf("active console = %d, want 1..63", active)
		}
		if !p.VTExists(active) {
			t.Errorf("active console %d should have a device node", active)
		}
		if !p.VTBusy(active) {
			t.Errorf("active console %d should be busy (some process holds it)", active)
		}
		if _, err := p.Pick(active); err == nil {
			t.Errorf("Pick on the active console %d must be refused", active)
		}
	}

	// A free VT is pickable: scan for one, and if the machine keeps all its
	// terminals busy (tiny VM), the loop below simply finds none.
	for n := 1; n <= 63; n++ {
		if picked, err := p.Pick(n); err == nil {
			if picked != n {
				t.Errorf("Pick(%d) = %d, want %d unchanged", n, picked, n)
			}
			return
		}
	}
	t.Log("all virtual terminals are busy on this machine; nothing to pick")
}

// The existence probe must not disturb the terminal it opens: O_NOCTTY means
// the probe never becomes the controlling terminal of the session.
func TestProbeOpenDoesNotTakeControl(t *testing.T) {
	before := controllingTTYOfSelf(t)
	p := vt.NewProbe()
	for n := 1; n <= 8; n++ {
		p.VTExists(n) // any answer is fine; the point is the side effect
	}
	if after := controllingTTYOfSelf(t); after != before {
		t.Errorf("controlling tty changed from %q to %q: the probe must not seize a terminal", before, after)
	}
}

// controllingTTYOfSelf reads this test process's own controlling terminal out
// of /proc/self/stat (field 7; comm may contain spaces, so parsing anchors
// after the last ')').
func controllingTTYOfSelf(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Skipf("cannot read /proc/self/stat: %v", err)
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		t.Skip("unexpected /proc/self/stat layout")
	}
	fields := strings.Fields(s[i+1:])
	if len(fields) < 5 {
		t.Skip("unexpected /proc/self/stat layout")
	}
	return fields[4]
}
