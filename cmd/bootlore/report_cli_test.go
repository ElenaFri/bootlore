package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
)

// --- report command (CLI-level; the end-to-end exports live in the system tier) ---

func TestReportTextToStdout(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"report"}, strings.NewReader(twoEntries), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "BootLore diagnostic report") {
		t.Errorf("text report should carry its title:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Boot summary") {
		t.Errorf("text report should include the boot summary:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Failed with result") {
		t.Errorf("text report should list the boot's failures:\n%s", stdout)
	}
}

func TestReportJSONToStdout(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"report", "--format", "json"}, strings.NewReader(twoEntries), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("json output does not parse: %v\n%s", err, stdout)
	}
}

func TestReportToFileWritesAndStaysQuiet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-report.md")
	stdout, stderr, code := runCLI([]string{"report", "--format", "markdown", "--output", path}, strings.NewReader(twoEntries), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should stay empty on a file export, got %q", stdout)
	}
	if !strings.Contains(stderr, path) {
		t.Errorf("stderr should confirm the path, got %q", stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Fatalf("report file missing or empty: %v", err)
	}
}

func TestReportUnknownFormatFails(t *testing.T) {
	_, stderr, code := runCLI([]string{"report", "--format", "pdf"}, strings.NewReader(twoEntries), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (unknown format is a usage error)", code)
	}
	if !strings.Contains(stderr, "pdf") {
		t.Errorf("stderr should name the rejected format, got %q", stderr)
	}
}

func TestReportBackendUnavailableShowsHint(t *testing.T) {
	fr := &collectortest.FakeRunner{Err: fmt.Errorf("collected: reading journal: %w", collector.ErrBackendUnavailable)}
	_, stderr, code := runCLI([]string{"report", "--journal"}, strings.NewReader(""), fr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("stderr should contain the capture hint, got %q", stderr)
	}
}
