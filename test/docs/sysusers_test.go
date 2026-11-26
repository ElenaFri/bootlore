// Guarding the declarative system account: shipping a sysusers.d file lets the
// bootlore account be created automatically at install time (and re-ensured at
// boot by systemd-sysusers.service), so no manual useradd is ever needed. The
// account name must match the unit's User=. File reads only, like the other
// drift checks.
package docs

import (
	"regexp"
	"strings"
	"testing"
)

const sysusersFile = "../../systemd/bootlore.sysusers.conf"

func TestSysusersFileDeclaresTheAccount(t *testing.T) {
	conf := readFile(t, sysusersFile)
	// A sysusers.d "u" line creating the bootlore user.
	if !regexp.MustCompile(`(?m)^u\s+bootlore\b`).MatchString(conf) {
		t.Errorf("sysusers file must declare the bootlore user with a 'u' line:\n%s", conf)
	}
}

// The account the sysusers file creates must be exactly the one the unit runs
// as; otherwise the service would still fail with 217/USER at boot.
func TestSysusersAccountMatchesUnitUser(t *testing.T) {
	conf := readFile(t, sysusersFile)
	unit := readFile(t, unitFile)

	sys := regexp.MustCompile(`(?m)^u\s+(\S+)`).FindStringSubmatch(conf)
	usr := regexp.MustCompile(`(?m)^User=(\S+)`).FindStringSubmatch(unit)
	if sys == nil {
		t.Fatal("no 'u' line in the sysusers file")
	}
	if usr == nil {
		t.Fatal("no User= in the unit file")
	}
	if sys[1] != usr[1] {
		t.Errorf("sysusers creates %q but the unit runs as %q: they must match", sys[1], usr[1])
	}
}

// Installation must ship the sysusers file and create the account, so a fresh
// user never has to run useradd by hand.
func TestInstallCreatesAccountAutomatically(t *testing.T) {
	mk := readFile(t, makefile)
	install := targetBody(mk, "install")

	if !strings.Contains(install, "sysusers.conf") && !strings.Contains(install, "SYSUSERSDIR") {
		t.Error("install target should install the sysusers.d file")
	}
	if !strings.Contains(install, "systemd-sysusers") {
		t.Error("install target should run systemd-sysusers to create the account")
	}
	if !regexp.MustCompile(`SYSUSERSDIR\s*\?=.*sysusers\.d`).MatchString(mk) {
		t.Error("Makefile should define SYSUSERSDIR pointing at a sysusers.d directory")
	}
}

// The README must reflect that the account is created automatically, not by a
// manual useradd.
func TestReadmeDropsManualUseradd(t *testing.T) {
	readme := readFile(t, "../../README.md")
	if !regexp.MustCompile(`(?i)sysusers|automatically`).MatchString(readme) {
		t.Error("README should explain the account is created automatically (systemd-sysusers)")
	}
}
