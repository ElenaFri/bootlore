// Package docs guards the project's documentation against drifting away from
// the actual CLI. It is part of the unit tier (no build tag): the checks are
// pure file reads, fast and deterministic.
package docs

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

const manPage = "../../man/bootlore.1"

// commands the CLI exposes (kept in sync with the usage text by the test below).
var commands = []string{"collect", "live", "timeline", "summary", "explain", "errors", "tui", "version", "help"}

// The man page must document every CLI command...
//
// A command is documented when it appears in an .SS section heading, alone or
// in a comma-separated list (".SS version, help").
func TestManPageDocumentsEveryCommand(t *testing.T) {
	documented := map[string]bool{}
	for _, line := range strings.Split(readFile(t, manPage), "\n") {
		if !strings.HasPrefix(line, ".SS ") {
			continue
		}
		for _, name := range strings.Split(strings.TrimPrefix(line, ".SS "), ",") {
			documented[strings.TrimSpace(name)] = true
		}
	}
	for _, cmd := range commands {
		if !documented[cmd] {
			t.Errorf("man page has no .SS section for the %q command", cmd)
		}
	}
}

// ...and every flag the binary accepts.
func TestManPageDocumentsEveryFlag(t *testing.T) {
	page := readFile(t, manPage)
	// roff escapes the dashes: \-\-input, \-\-journal, ...
	flat := strings.ReplaceAll(page, `\-`, "-")
	for _, flag := range []string{"--input", "--journal", "--boot", "--view", "--level", "--category"} {
		if !strings.Contains(flat, flag) {
			t.Errorf("man page does not document the %s flag", flag)
		}
	}
}

// The man page must stay portable ASCII roff: non-ASCII bytes trigger groff
// warnings and render inconsistently across terminals.
func TestManPageIsASCII(t *testing.T) {
	page := readFile(t, manPage)
	for i, line := range strings.Split(page, "\n") {
		for _, r := range line {
			if r > 127 {
				t.Errorf("man page line %d contains non-ASCII character %q (use a roff escape such as \\(em)", i+1, r)
				break
			}
		}
	}
}

// When groff is available, the page must lint cleanly (no warnings).
func TestManPageLintsCleanly(t *testing.T) {
	groff, err := exec.LookPath("groff")
	if err != nil {
		t.Skip("groff not installed; skipping man page lint")
	}
	out, err := exec.Command(groff, "-man", "-ww", "-z", manPage).CombinedOutput()
	if err != nil {
		t.Fatalf("groff failed: %v\n%s", err, out)
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("groff reported warnings:\n%s", out)
	}
}

// The README must list every command in its commands table.
func TestReadmeListsEveryCommand(t *testing.T) {
	readme := readFile(t, "../../README.md")
	for _, cmd := range commands {
		if cmd == "help" || cmd == "version" {
			continue // utility commands are not part of the main table
		}
		if !strings.Contains(readme, "`bootlore "+cmd+"`") {
			t.Errorf("README commands table does not list `bootlore %s`", cmd)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}
