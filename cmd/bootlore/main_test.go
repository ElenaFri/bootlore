package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
)

const twoEntries = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","MESSAGE":"Reached target Multi-User System."}
{"__REALTIME_TIMESTAMP":"1700000001000000","PRIORITY":"4","SYSLOG_IDENTIFIER":"systemd","_PID":"1","MESSAGE":"unit.service: Failed with result 'exit-code'."}`

// runCLI invokes runWith with buffers and a collector backed by fr.
func runCLI(args []string, stdin io.Reader, fr *collectortest.FakeRunner) (stdout, stderr string, code int) {
	var out, errb bytes.Buffer
	code = runWith(args, stdin, &out, &errb, collector.New(fr))
	return out.String(), errb.String(), code
}

// liveCLI invokes runWith with a collector backed by a sequence of outputs, as
// the replay-then-live transition produces two backend invocations.
func liveCLI(args []string, outputs ...string) (stdout, stderr string, code int) {
	sr := &collectortest.SequenceRunner{Outputs: outputs}
	var out, errb bytes.Buffer
	code = runWith(args, strings.NewReader(""), &out, &errb, collector.New(sr))
	return out.String(), errb.String(), code
}

func TestRunNoArgsShowsUsage(t *testing.T) {
	stdout, stderr, code := runCLI(nil, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr should contain usage, got %q", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout should be empty, got %q", stdout)
	}
}

func TestRunVersion(t *testing.T) {
	stdout, _, code := runCLI([]string{"version"}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(stdout, "bootlore ") {
		t.Errorf("version output = %q", stdout)
	}
}

func TestRunHelp(t *testing.T) {
	stdout, _, code := runCLI([]string{"help"}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Commands:") {
		t.Errorf("help output should list commands, got %q", stdout)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	_, stderr, code := runCLI([]string{"frobnicate"}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, `unknown command "frobnicate"`) {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCollectFromStdin(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"collect"}, strings.NewReader(twoEntries), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), stdout)
	}
	if !strings.Contains(lines[0], "systemd[1]: Reached target Multi-User System.") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "[warning] systemd[1]: unit.service: Failed") {
		t.Errorf("second line = %q", lines[1])
	}
}

func TestCollectFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot.json")
	if err := os.WriteFile(path, []byte(twoEntries), 0o644); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	stdout, _, code := runCLI([]string{"collect", "--input", path}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Reached target Multi-User System.") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestCollectFromFileError(t *testing.T) {
	_, stderr, code := runCLI([]string{"collect", "--input", "/nonexistent/boot.json"}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "/nonexistent/boot.json") {
		t.Errorf("stderr should mention the file, got %q", stderr)
	}
}

func TestCollectFromJournalFlag(t *testing.T) {
	fr := &collectortest.FakeRunner{Output: twoEntries}
	stdout, stderr, code := runCLI([]string{"collect", "--journal"}, strings.NewReader(""), fr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "Reached target Multi-User System.") {
		t.Errorf("stdout = %q", stdout)
	}
}

// In a terminal (stdin not piped), collect defaults to reading the journal.
func TestCollectDefaultsToJournalWithoutPipe(t *testing.T) {
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { devnull.Close() })

	fr := &collectortest.FakeRunner{Output: twoEntries}
	stdout, _, code := runCLI([]string{"collect"}, devnull, fr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "Reached target Multi-User System.") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestCollectJournalBackendUnavailableShowsHint(t *testing.T) {
	fr := &collectortest.FakeRunner{Err: fmt.Errorf("collector: reading journal: %w", collector.ErrBackendUnavailable)}
	_, stderr, code := runCLI([]string{"collect", "--journal"}, strings.NewReader(""), fr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("stderr should contain the pipe hint, got %q", stderr)
	}
}

func TestCollectMalformedInputFails(t *testing.T) {
	_, stderr, code := runCLI([]string{"collect"}, strings.NewReader("{not json}"), &collectortest.FakeRunner{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "bootlore:") {
		t.Errorf("stderr should start with bootlore:, got %q", stderr)
	}
	if strings.Contains(stderr, "hint:") {
		t.Errorf("parse errors should not print the backend hint, got %q", stderr)
	}
}

func TestCollectBadFlagFails(t *testing.T) {
	_, _, code := runCLI([]string{"collect", "--bogus"}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// The live command must first replay the boot's existing events, then stream
// the live ones, in order, in one continuous output.
func TestLiveReplaysThenFollows(t *testing.T) {
	replay := `{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","MESSAGE":"Reached target Multi-User System."}`
	lived := `{"__REALTIME_TIMESTAMP":"1700000001000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"NetworkManager","_PID":"412","MESSAGE":"Started Network Manager."}`

	stdout, stderr, code := liveCLI([]string{"live"}, replay, lived)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), stdout)
	}
	if !strings.Contains(lines[0], "Reached target Multi-User System.") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[1], "Started Network Manager.") {
		t.Errorf("second line = %q", lines[1])
	}
}

func TestLiveBackendErrorFails(t *testing.T) {
	fr := &collectortest.FakeRunner{Err: fmt.Errorf("collector: reading journal: %w", collector.ErrBackendUnavailable)}
	_, stderr, code := runCLI([]string{"live"}, strings.NewReader(""), fr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("stderr should contain the live hint, got %q", stderr)
	}
}

func TestLiveBadFlagFails(t *testing.T) {
	_, _, code := liveCLI([]string{"live", "--bogus"})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestStdinIsPiped(t *testing.T) {
	if !stdinIsPiped(strings.NewReader("")) {
		t.Error("strings.Reader should count as piped")
	}
	if !stdinIsPiped(&bytes.Buffer{}) {
		t.Error("bytes.Buffer should count as piped")
	}

	f, err := os.Open(os.DevNull) // character device, like a terminal
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	t.Cleanup(func() { f.Close() })
	if stdinIsPiped(f) {
		t.Errorf("%s (char device) should count as not piped", os.DevNull)
	}
}

// A closed *os.File cannot be stat'ed; it must be treated as piped rather
// than sending BootLore to the journal unexpectedly.
func TestStdinIsPipedClosedFile(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("opening %s: %v", os.DevNull, err)
	}
	f.Close()
	if !stdinIsPiped(f) {
		t.Error("closed file (Stat fails) should count as piped")
	}
}

// run() wires the real exec-backed collector. With piped stdin the collector is
// never invoked, so the production wiring can be exercised without journalctl.
func TestRunServesPipedStdinWithProductionWiring(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"collect"}, strings.NewReader(twoEntries), &out, &errb)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "Reached target Multi-User System.") {
		t.Errorf("stdout = %q", out.String())
	}
}
