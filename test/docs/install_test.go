// Guarding the install/uninstall story: the Makefile must ship targets that
// place every artifact where the systemd unit and the man page expect them,
// and the README must document the procedure (including the SELinux pitfall
// of running the binary from a home directory). File reads only, like the
// other drift checks.
package docs

import (
	"regexp"
	"strings"
	"testing"
)

const makefile = "../../Makefile"

func TestMakefileHasInstallTargets(t *testing.T) {
	mk := readFile(t, makefile)
	for _, target := range []string{"install", "uninstall"} {
		if !regexp.MustCompile(`(?m)^` + target + `:`).MatchString(mk) {
			t.Errorf("Makefile has no %q target", target)
		}
		if !strings.Contains(mk, target+" ") && !regexp.MustCompile(`\b`+target+`\b`).MatchString(declaredPhony(mk)) {
			t.Errorf("%q is not declared .PHONY", target)
		}
	}
}

// install must place the binary where the shipped unit's ExecStart points
// (/usr/bin/bootlore), the man page under a man1 directory, and the unit into
// a systemd unit directory. Overridable prefixes are fine as long as the
// defaults line up with the unit file.
func TestInstallPlacesArtifactsConsistently(t *testing.T) {
	mk := readFile(t, makefile)
	unit := readFile(t, unitFile)

	execStart := regexp.MustCompile(`(?m)^ExecStart=(\S+)`).FindStringSubmatch(unit)
	if execStart == nil {
		t.Fatal("unit has no ExecStart to line up with")
	}
	binPath := execStart[1] // /usr/bin/bootlore

	// The install target must install the binary, the unit and the man page,
	// referencing the layout variables defined at the top of the Makefile.
	install := targetBody(mk, "install")
	if install == "" {
		t.Fatal("no install target body found")
	}
	if !strings.Contains(install, "bootlore") {
		t.Error("install target should install the bootlore binary")
	}
	if !strings.Contains(install, "UNITDIR") || !strings.Contains(install, "bootlore.service") {
		t.Error("install target should install the systemd unit into UNITDIR")
	}
	if !strings.Contains(install, "MANDIR") || !strings.Contains(install, "bootlore.1") {
		t.Error("install target should install the man page into MANDIR")
	}
	// MANDIR and UNITDIR defaults must point at a man1 dir and a systemd unit
	// dir respectively.
	if !regexp.MustCompile(`MANDIR\s*\?=.*man1`).MatchString(mk) {
		t.Error("MANDIR default should end in man1")
	}
	if !regexp.MustCompile(`UNITDIR\s*\?=.*systemd`).MatchString(mk) {
		t.Error("UNITDIR default should point at a systemd unit directory")
	}
	// The default binary destination must be /usr (so /usr/bin/bootlore),
	// matching the unit's ExecStart, not /usr/local.
	if !strings.HasPrefix(binPath, "/usr/bin/") {
		t.Errorf("unit ExecStart is %q; the install default should target /usr/bin to match", binPath)
	}
	if !regexp.MustCompile(`PREFIX\s*\?=\s*/usr\b`).MatchString(mk) {
		t.Error("Makefile should default PREFIX to /usr so the binary lands in /usr/bin like the unit expects")
	}
}

func TestUninstallRemovesWhatInstallAdded(t *testing.T) {
	uninstall := targetBody(readFile(t, makefile), "uninstall")
	if uninstall == "" {
		t.Fatal("no uninstall target body found")
	}
	if !strings.Contains(uninstall, "rm") {
		t.Error("uninstall should remove the installed files")
	}
	for _, artifact := range []string{"bootlore", "MANDIR", "UNITDIR"} {
		if !strings.Contains(uninstall, artifact) {
			t.Errorf("uninstall should remove the %s artifact", artifact)
		}
	}
}

// The README must spell out installation, removal and the SELinux caveat that
// bit us: a service cannot exec a binary left in a home directory.
func TestReadmeDocumentsInstallAndRemoval(t *testing.T) {
	readme := readFile(t, "../../README.md")
	if !strings.Contains(readme, "make install") {
		t.Error("README should document `make install`")
	}
	if !strings.Contains(readme, "make uninstall") {
		t.Error("README should document `make uninstall`")
	}
	if !regexp.MustCompile(`(?i)selinux`).MatchString(readme) {
		t.Error("README should warn about the SELinux home-directory exec pitfall")
	}
}

// declaredPhony returns the text of the .PHONY declaration, following `\`
// line continuations.
func declaredPhony(mk string) string {
	var b strings.Builder
	cont := false
	for _, line := range strings.Split(mk, "\n") {
		start := strings.HasPrefix(line, ".PHONY:")
		if start || cont {
			b.WriteString(line)
			b.WriteString(" ")
		}
		cont = (start || cont) && strings.HasSuffix(strings.TrimRight(line, " \t"), "\\")
	}
	return b.String()
}

// targetBody returns the recipe lines of a Makefile target (the indented
// block following "target:"), up to the next unindented line.
func targetBody(mk, target string) string {
	lines := strings.Split(mk, "\n")
	var body strings.Builder
	in := false
	for _, line := range lines {
		if regexp.MustCompile(`^` + target + `:`).MatchString(line) {
			in = true
			continue
		}
		if in {
			if line == "" || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
				body.WriteString(line)
				body.WriteString("\n")
				continue
			}
			break
		}
	}
	return body.String()
}
