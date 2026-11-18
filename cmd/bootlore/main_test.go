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

// assertLines splits stdout into lines (ignoring a trailing newline) and checks
// that each line contains the corresponding wanted substring.
func assertLines(t *testing.T, stdout string, wantSubstrings ...string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != len(wantSubstrings) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(wantSubstrings), stdout)
	}
	for i, want := range wantSubstrings {
		if !strings.Contains(lines[i], want) {
			t.Errorf("line %d = %q, want to contain %q", i, lines[i], want)
		}
	}
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
	assertLines(t, stdout,
		"systemd[1]: Reached target Multi-User System.",
		"[warning] systemd[1]: unit.service: Failed")
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
	assertLines(t, stdout,
		"Reached target Multi-User System.",
		"Started Network Manager.")
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

// --- backfilled branches ---

func TestLiveViewInvalidFails(t *testing.T) {
	_, _, code := liveCLI([]string{"live", "--view", "bogus"})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestExplainBackendUnavailableShowsHint(t *testing.T) {
	fr := &collectortest.FakeRunner{Err: fmt.Errorf("collector: reading journal: %w", collector.ErrBackendUnavailable)}
	_, stderr, code := runCLI([]string{"explain", "--journal"}, strings.NewReader(""), fr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("stderr should contain the explain hint, got %q", stderr)
	}
}

func TestExplainFileErrorFails(t *testing.T) {
	_, stderr, code := runCLI([]string{"explain", "--input", "/nonexistent/boot.json"}, strings.NewReader(""), &collectortest.FakeRunner{})
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "/nonexistent/boot.json") {
		t.Errorf("stderr should mention the file, got %q", stderr)
	}
}

// --- errors command (noteworthy events only) ---

const failureEntry = `{"__REALTIME_TIMESTAMP":"1700000002000000","PRIORITY":"4","SYSLOG_IDENTIFIER":"systemd","_PID":"1","_SYSTEMD_UNIT":"bluetooth.service","MESSAGE":"bluetooth.service: Failed with result 'exit-code'."}`

func TestErrorsShowsOnlyNoteworthyEvents(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"errors"}, strings.NewReader(nmEntry+"\n"+failureEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	assertLines(t, stdout, "[failure] SYSTEMD: A unit reported a failure. The cause is not stated here and needs investigation.")
	if strings.Contains(stdout, "Network Manager") {
		t.Errorf("normal events must be filtered out:\n%s", stdout)
	}
}

func TestErrorsNoneFound(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"errors"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "No warnings or failures found.") {
		t.Errorf("stdout should say that nothing noteworthy was found:\n%s", stdout)
	}
}

func TestErrorsViewLog(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"errors", "--view", "log"}, strings.NewReader(failureEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "[warning] systemd[1]: bluetooth.service: Failed with result 'exit-code'.") {
		t.Errorf("log view should show the original line:\n%s", stdout)
	}
}

func TestErrorsViewInvalidFails(t *testing.T) {
	_, _, code := runCLI([]string{"errors", "--view", "bogus"}, strings.NewReader(failureEntry), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestErrorsBackendUnavailableShowsHint(t *testing.T) {
	fr := &collectortest.FakeRunner{Err: fmt.Errorf("collector: reading journal: %w", collector.ErrBackendUnavailable)}
	_, stderr, code := runCLI([]string{"errors", "--journal"}, strings.NewReader(""), fr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "hint:") {
		t.Errorf("stderr should contain the hint, got %q", stderr)
	}
}

// --- timeline command ---

const shuffledMonotonic = `{"__MONOTONIC_TIMESTAMP":"2000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"third"}
{"__MONOTONIC_TIMESTAMP":"500000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"first"}
{"__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"second"}`

// The timeline must order events by their monotonic time since boot, not by
// their order of arrival in the stream.
func TestTimelineSortedByMonotonic(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"timeline"}, strings.NewReader(shuffledMonotonic), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	assertLines(t, stdout,
		"+0.500s [normal] SYSTEMD: first",
		"+1.200s [normal] SYSTEMD: second",
		"+2.000s [normal] SYSTEMD: third")
}

// When monotonic timestamps are missing, ordering falls back to wall-clock
// time (defensive sort — the real journal always provides monotonic values).
func TestTimelineFallbackToWallClock(t *testing.T) {
	in := `{"__REALTIME_TIMESTAMP":"1700000002000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"late"}
{"__REALTIME_TIMESTAMP":"1700000001000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"early"}`
	stdout, stderr, code := runCLI([]string{"timeline"}, strings.NewReader(in), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	assertLines(t, stdout,
		"- [normal] SYSTEMD: early",
		"- [normal] SYSTEMD: late")
}

func TestTimelineViewLog(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"timeline", "--view", "log"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	assertLines(t, stdout, "+1.200s [info] systemd[1]: Started Network Manager.")
}

func TestTimelineViewInvalidFails(t *testing.T) {
	_, _, code := runCLI([]string{"timeline", "--view", "bogus"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// --- HUMAN view (--view human) ---

const nmEntry = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","_SYSTEMD_UNIT":"NetworkManager.service","MESSAGE":"Started Network Manager."}`

func TestCollectViewHuman(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"collect", "--view", "human"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	want := "2023-11-14T22:13:20Z [normal] NETWORK: A systemd unit changed state as part of the boot sequence."
	assertLines(t, stdout, want)
}

func TestCollectViewInvalidFails(t *testing.T) {
	_, _, code := runCLI([]string{"collect", "--view", "bogus"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestLiveViewHuman(t *testing.T) {
	replay := `{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Reached target Multi-User System."}`
	stdout, stderr, code := liveCLI([]string{"live", "--view", "human"}, replay, nmEntry)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	assertLines(t, stdout,
		"[normal] SYSTEMD: A boot milestone was reached: all units required for this stage are active.",
		"[normal] NETWORK: A systemd unit changed state as part of the boot sequence.")
}

// --- explain command ---

const opaqueEntry = `{"MESSAGE":"opaque daemon thing"}`

func TestExplainDefaultLevelIsTechnical(t *testing.T) {
	in := nmEntry + "\n" + opaqueEntry
	stdout, stderr, code := runCLI([]string{"explain"}, strings.NewReader(in), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (two events + one explanation):\n%s", len(lines), stdout)
	}
	if !strings.Contains(lines[0], "NETWORK: A systemd unit changed state") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  systemd executes start jobs in dependency order") {
		t.Errorf("second line should be the indented technical explanation: %q", lines[1])
	}
	if !strings.Contains(lines[2], "opaque daemon thing") {
		t.Errorf("unexplained event should still be listed: %q", lines[2])
	}
}

func TestExplainLevelOverview(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"explain", "--level", "overview"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "  systemd starts the system piece by piece") {
		t.Errorf("expected the indented overview explanation:\n%s", stdout)
	}
}

func TestExplainLevelInDepth(t *testing.T) {
	stdout, stderr, code := runCLI([]string{"explain", "--level", "in-depth"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "  What counts as 'started' depends") {
		t.Errorf("expected the indented in-depth explanation:\n%s", stdout)
	}
}

func TestExplainBadLevelFails(t *testing.T) {
	_, _, code := runCLI([]string{"explain", "--level", "bogus"}, strings.NewReader(nmEntry), &collectortest.FakeRunner{})
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
