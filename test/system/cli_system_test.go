//go:build system

// Package system contains end-to-end tests that build the real bootlore binary
// and run it as a separate process, exercising the full CLI the way a user
// would.
//
// Run with: go test -tags=system ./test/system/...
package system

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collectortest"
)

var (
	binPath     string
	moduleRoot  string
	fixturePath string
)

// TestMain builds the bootlore binary once for the whole suite.
func TestMain(m *testing.M) {
	root, err := findModuleRoot()
	if err != nil {
		panic("locating module root: " + err.Error())
	}
	moduleRoot = root
	fixturePath = filepath.Join(moduleRoot, "test", "testdata", "boot-journal.json")

	dir, err := os.MkdirTemp("", "bootlore-system")
	if err != nil {
		panic("temp dir: " + err.Error())
	}
	defer os.RemoveAll(dir)

	binPath = filepath.Join(dir, "bootlore")
	build := exec.Command("go", "build", "-o", binPath, "./cmd/bootlore")
	build.Dir = moduleRoot
	if out, err := build.CombinedOutput(); err != nil {
		panic("building bootlore:\n" + string(out))
	}

	os.Exit(m.Run())
}

func findModuleRoot() (string, error) {
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}

type result struct {
	stdout string
	stderr string
	code   int
}

// runBootlore executes the built binary with the given args and stdin.
func runBootlore(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if ok := asExitError(err, &exitErr); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("running bootlore %v: %v", args, err)
		}
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

func asExitError(err error, target **exec.ExitError) bool {
	if e, ok := err.(*exec.ExitError); ok {
		*target = e
		return true
	}
	return false
}

func TestCollectFromFile(t *testing.T) {
	res := runBootlore(t, "", "collect", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	lines := nonEmptyLines(res.stdout)
	if len(lines) != 5 {
		t.Fatalf("got %d output lines, want 5:\n%s", len(lines), res.stdout)
	}
	if !strings.Contains(lines[0], "kernel: Linux version 6.6.8-arch1-1") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(res.stdout, "NetworkManager[412]: Started Network Manager.") {
		t.Errorf("output missing NetworkManager line:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "[warning] systemd[1]: bluetooth.service: Failed") {
		t.Errorf("output missing bluetooth warning line:\n%s", res.stdout)
	}
}

// Reading from stdin must produce exactly the same output as reading a file.
func TestCollectFromStdinMatchesFile(t *testing.T) {
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	fromStdin := runBootlore(t, string(data), "collect")
	fromFile := runBootlore(t, "", "collect", "--input", fixturePath)
	if fromStdin.code != 0 {
		t.Fatalf("stdin exit code = %d, stderr = %q", fromStdin.code, fromStdin.stderr)
	}
	if fromStdin.stdout != fromFile.stdout {
		t.Errorf("stdin and file output differ:\nstdin:\n%s\nfile:\n%s", fromStdin.stdout, fromFile.stdout)
	}
}

func TestCollectEmptyInput(t *testing.T) {
	res := runBootlore(t, "", "collect")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if strings.TrimSpace(res.stdout) != "" {
		t.Errorf("expected empty output, got %q", res.stdout)
	}
}

func TestCollectMalformedInputFails(t *testing.T) {
	res := runBootlore(t, "{not json}\n", "collect")
	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", res.code, res.stderr)
	}
	if res.stderr == "" {
		t.Error("expected an error message on stderr")
	}
}

func TestSummaryFromFile(t *testing.T) {
	res := runBootlore(t, "", "summary", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	for _, want := range []string{"Boot summary", "9f1c2d", "+6.000s"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("summary missing %q:\n%s", want, res.stdout)
		}
	}
	if !regexp.MustCompile(`Events:\s+5`).MatchString(res.stdout) {
		t.Errorf("summary should report 5 events:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`Failures:\s+1`).MatchString(res.stdout) {
		t.Errorf("summary should report 1 failure:\n%s", res.stdout)
	}
}

func TestServicesFromFile(t *testing.T) {
	res := runBootlore(t, "", "services", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "Failed services") || !strings.Contains(res.stdout, "bluetooth.service") {
		t.Errorf("services should report the failed bluetooth unit:\n%s", res.stdout)
	}
}

// The fixtures carry no "Startup finished" line, so timing is exercised with a
// crafted boot piped over stdin that includes systemd's authoritative summary.
func TestTimingFromStdin(t *testing.T) {
	stdin := `{"__MONOTONIC_TIMESTAMP":"2000000","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Reached target Basic System."}
{"__MONOTONIC_TIMESTAMP":"5000000","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Startup finished in 1.500s (kernel) + 3.500s (userspace) = 5.000s"}`
	res := runBootlore(t, stdin, "timing")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	for _, re := range []string{`Total:\s+5\.000s`, `reported by systemd`, `kernel\s+1\.500s`, `\+2\.000s\s+Basic System`} {
		if !regexp.MustCompile(re).MatchString(res.stdout) {
			t.Errorf("timing report does not match %q:\n%s", re, res.stdout)
		}
	}
}

// The per-user systemd manager prints its own "Startup finished" (e.g. 99ms for
// the session) and session targets at large offsets since boot. End to end,
// timing must report the SYSTEM manager's total and milestones only, never the
// user session's, so a short session total cannot sit beside boot-long targets.
func TestTimingIgnoresUserManager(t *testing.T) {
	stdin := `{"__MONOTONIC_TIMESTAMP":"99000","_UID":"1000","_SYSTEMD_UNIT":"user@1000.service","_SYSTEMD_USER_UNIT":"init.scope","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Startup finished in 99ms."}
{"__MONOTONIC_TIMESTAMP":"222290000","_UID":"1000","_SYSTEMD_UNIT":"user@1000.service","_SYSTEMD_USER_UNIT":"init.scope","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Reached target Bluetooth."}
{"__MONOTONIC_TIMESTAMP":"2000000","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Reached target Basic System."}
{"__MONOTONIC_TIMESTAMP":"5000000","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Startup finished in 1.500s (kernel) + 3.500s (userspace) = 5.000s"}`
	res := runBootlore(t, stdin, "timing")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	for _, re := range []string{`Total:\s+5\.000s`, `reported by systemd`, `\+2\.000s\s+Basic System`} {
		if !regexp.MustCompile(re).MatchString(res.stdout) {
			t.Errorf("timing report does not match %q:\n%s", re, res.stdout)
		}
	}
	for _, re := range []string{`99ms`, `Bluetooth`} {
		if regexp.MustCompile(re).MatchString(res.stdout) {
			t.Errorf("timing report must not contain user-session data %q:\n%s", re, res.stdout)
		}
	}
}

// When every event comes from the per-user manager, the real binary must warn on
// stderr that only the user-session journal is visible and point at the
// systemd-journal group, rather than passing a meaningless total off as a boot
// time on stdout.
func TestTimingWarnsUserOnlyJournal(t *testing.T) {
	stdin := `{"__MONOTONIC_TIMESTAMP":"99000","_UID":"1000","_SYSTEMD_UNIT":"user@1000.service","_SYSTEMD_USER_UNIT":"init.scope","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Startup finished in 99ms."}
{"__MONOTONIC_TIMESTAMP":"7388586","_UID":"1000","_SYSTEMD_UNIT":"user@1000.service","_SYSTEMD_USER_UNIT":"init.scope","USER_UNIT":"paths.target","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Reached target Paths."}`
	res := runBootlore(t, stdin, "timing")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "systemd-journal") {
		t.Errorf("stderr should point at the systemd-journal group, got %q", res.stderr)
	}
	if strings.Contains(res.stdout, "Paths") {
		t.Errorf("user-session target leaked into the report:\n%s", res.stdout)
	}
}

// compare contrasts two captured boots: the main fixture (9f1c2d, bluetooth
// failed) as the current boot against the user-session fixture (c0ffee,
// xdg-desktop-portal failed) as the baseline.
func TestCompareFromFiles(t *testing.T) {
	baseline := filepath.Join(moduleRoot, "test", "testdata", "user-session-journal.json")
	res := runBootlore(t, "", "compare", "--input", fixturePath, "--against-input", baseline)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "Boot comparison") {
		t.Errorf("missing comparison header:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`Boot ID:\s+c0ffee\s+9f1c2d`).MatchString(res.stdout) {
		t.Errorf("comparison should name both boots (baseline c0ffee, current 9f1c2d):\n%s", res.stdout)
	}
	if !regexp.MustCompile(`fixed:\s+xdg-desktop-portal\.service`).MatchString(res.stdout) {
		t.Errorf("xdg-desktop-portal.service should be reported as fixed:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`new:\s+bluetooth\.service`).MatchString(res.stdout) {
		t.Errorf("bluetooth.service should be reported as newly failing:\n%s", res.stdout)
	}
}

// Transient run-* units (systemd-run/run0) must be kept out of the service diff
// yet counted, end to end: the real units are diffed, the throwaway ones are not
// named, and the transient note reports how many were excluded per side.
func TestCompareExcludesTransientUnits(t *testing.T) {
	baseline := filepath.Join(moduleRoot, "test", "testdata", "compare-transient-baseline.json")
	current := filepath.Join(moduleRoot, "test", "testdata", "compare-transient-current.json")
	res := runBootlore(t, "", "compare", "--input", current, "--against-input", baseline)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !regexp.MustCompile(`fixed:\s+bluetooth\.service`).MatchString(res.stdout) {
		t.Errorf("bluetooth.service should be reported as fixed:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`new:\s+cups\.service`).MatchString(res.stdout) {
		t.Errorf("cups.service should be reported as newly failing:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout, "run-p100") || strings.Contains(res.stdout, "run-p300") || strings.Contains(res.stdout, "run-p500") {
		t.Errorf("transient run-* units should not appear in the diff:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`transient:\s+1 baseline, 2 current`).MatchString(res.stdout) {
		t.Errorf("report should count the excluded transient units (1 baseline, 2 current):\n%s", res.stdout)
	}
}

// A flat duration delta can mislead: a boot that is slower overall may be slower
// only in firmware or the boot loader, with Linux userspace unchanged. End to
// end, the per-phase breakdown must expose that so the user does not blame Linux.
func TestCompareBreaksDownPhases(t *testing.T) {
	baseline := filepath.Join(moduleRoot, "test", "testdata", "compare-phases-baseline.json")
	current := filepath.Join(moduleRoot, "test", "testdata", "compare-phases-current.json")
	res := runBootlore(t, "", "compare", "--input", current, "--against-input", baseline)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !regexp.MustCompile(`Duration:\s+14\.170s\s+59\.470s\s+\+45\.300s\s+\(slower\)`).MatchString(res.stdout) {
		t.Errorf("duration delta should be reported:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`firmware:\s+5\.000s\s+43\.400s\s+\+38\.400s`).MatchString(res.stdout) {
		t.Errorf("firmware should carry the bulk of the slowdown:\n%s", res.stdout)
	}
	if !regexp.MustCompile(`userspace:\s+3\.900s\s+3\.900s\s+0\.000s`).MatchString(res.stdout) {
		t.Errorf("userspace should be unchanged, proving Linux did not regress:\n%s", res.stdout)
	}
}

// The report command bundles summary, timing and services into one document.
// The default text rendering must carry every section end to end.
func TestReportTextFromFile(t *testing.T) {
	res := runBootlore(t, "", "report", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	for _, want := range []string{"BootLore diagnostic report", "Boot summary", "Boot timing", "bluetooth.service"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("report missing %q:\n%s", want, res.stdout)
		}
	}
}

// --format json must emit a valid, documented document carrying the same
// diagnosis as the console report.
func TestReportJSONFromFile(t *testing.T) {
	res := runBootlore(t, "", "report", "--input", fixturePath, "--format", "json")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !json.Valid([]byte(res.stdout)) {
		t.Fatalf("report is not valid JSON:\n%s", res.stdout)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(res.stdout), &got); err != nil {
		t.Fatalf("decoding JSON report: %v", err)
	}
	boot := got["boot"].(map[string]any)
	if boot["events"] != float64(5) || boot["failures"] != float64(1) {
		t.Errorf("boot = %+v, want events 5 / failures 1", boot)
	}
}

// --format markdown must produce a shareable document with real headings.
func TestReportMarkdownFromFile(t *testing.T) {
	res := runBootlore(t, "", "report", "--input", fixturePath, "--format", "markdown")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.HasPrefix(res.stdout, "# BootLore diagnostic report") {
		t.Errorf("markdown report should start with the top-level heading:\n%s", res.stdout)
	}
	for _, want := range []string{"## Summary", "## Services", "bluetooth.service"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("markdown report missing %q:\n%s", want, res.stdout)
		}
	}
}

// --output exports the report to a file: stdout stays empty, stderr confirms the
// path, and the file holds the rendered document.
func TestReportToFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "boot-report.json")
	res := runBootlore(t, "", "report", "--input", fixturePath, "--format", "json", "--output", out)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if strings.TrimSpace(res.stdout) != "" {
		t.Errorf("stdout should be empty when exporting to a file, got %q", res.stdout)
	}
	if !strings.Contains(res.stderr, out) {
		t.Errorf("stderr should confirm the export path %q:\n%s", out, res.stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading exported report: %v", err)
	}
	if !json.Valid(data) {
		t.Errorf("exported report is not valid JSON:\n%s", data)
	}
}

// An unknown --format is a usage error (exit 2).
func TestReportUnknownFormatFails(t *testing.T) {
	res := runBootlore(t, "", "report", "--input", fixturePath, "--format", "yaml")
	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2 (stderr = %q)", res.code, res.stderr)
	}
	if res.stderr == "" {
		t.Error("expected an error message on stderr")
	}
}

func TestVersion(t *testing.T) {
	res := runBootlore(t, "", "version")
	if res.code != 0 {
		t.Fatalf("exit code = %d", res.code)
	}
	if !strings.HasPrefix(res.stdout, "bootlore ") {
		t.Errorf("version output = %q", res.stdout)
	}
}

func TestUnknownCommand(t *testing.T) {
	res := runBootlore(t, "", "frobnicate")
	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2", res.code)
	}
	if res.stderr == "" {
		t.Error("expected usage/error on stderr")
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// --- journald source (via a stub journalctl on PATH) ---

// runBootloreEnv runs the binary with a custom environment (used to control PATH
// so the stub journalctl is picked up instead of the real one).
func runBootloreEnv(t *testing.T, env []string, stdin string, args ...string) result {
	t.Helper()
	return runResult(exec.Command(binPath, args...), env, stdin)
}

// runResult runs a prepared command and captures its output and exit code.
func runResult(cmd *exec.Cmd, env []string, stdin string) result {
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if asExitError(err, &exitErr) {
			code = exitErr.ExitCode()
		}
	}
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

func TestCollectFromJournal(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, `printf '%s\n' '{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","MESSAGE":"Reached target Multi-User System."}'`)
	res := runBootloreEnv(t, []string{"PATH=" + dir}, "", "collect", "--journal")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "systemd[1]: Reached target Multi-User System.") {
		t.Errorf("unexpected journald output:\n%s", res.stdout)
	}
}

// A missing journalctl must fail gracefully with a helpful message, not crash.
func TestCollectJournalBackendMissing(t *testing.T) {
	res := runBootloreEnv(t, []string{"PATH=" + t.TempDir()}, "", "collect", "--journal")
	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "journalctl") {
		t.Errorf("stderr should mention journalctl: %q", res.stderr)
	}
}

// A failing journalctl (e.g. permission denied) must surface the backend error.
func TestCollectJournalBackendFailure(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, "echo 'Failed to add match: Permission denied' >&2\nexit 1\n")
	res := runBootloreEnv(t, []string{"PATH=" + dir}, "", "collect", "--journal")
	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1", res.code)
	}
	if !strings.Contains(res.stderr, "Permission denied") {
		t.Errorf("stderr should surface backend error: %q", res.stderr)
	}
}

// bootlore live must replay the boot's existing events and then stream live
// ones, in order, through two real journalctl invocations.
func TestLiveCommandReplaysThenFollows(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, collectortest.StubReplayThenLive)
	res := runBootloreEnv(t, []string{"PATH=" + dir}, "", "live")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "replayed 1") {
		t.Errorf("stdout should contain the replayed events:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "live event") {
		t.Errorf("stdout should contain the live event:\n%s", res.stdout)
	}
	if strings.Index(res.stdout, "replayed 1") > strings.Index(res.stdout, "live event") {
		t.Error("replayed events must be printed before live events")
	}
}

func TestLiveBadFlagFails(t *testing.T) {
	res := runBootlore(t, "", "live", "--bogus")
	if res.code != 2 {
		t.Errorf("exit code = %d, want 2", res.code)
	}
}

// --- HUMAN view and explain (end-to-end against the shared fixture) ---

func TestCollectViewHuman(t *testing.T) {
	res := runBootlore(t, "", "collect", "--view", "human", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	// This line is emitted by NetworkManager itself, not by systemd: it is
	// classified NETWORK but is not explained as a systemd unit lifecycle event,
	// so the HUMAN view falls back to the original message.
	if !strings.Contains(res.stdout, "[normal] NETWORK: Started Network Manager.") {
		t.Errorf("stdout should contain the NetworkManager line, classified but not mis-explained:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "[failure] SYSTEMD: A unit reported a failure.") {
		t.Errorf("stdout should contain the interpreted bluetooth failure line:\n%s", res.stdout)
	}
	// The LOG view stays the default and remains faithful to source data.
	resLog := runBootlore(t, "", "collect", "--input", fixturePath)
	if !strings.Contains(resLog.stdout, "bluetooth.service: Failed with result 'exit-code'.") {
		t.Errorf("default view should print the original message:\n%s", resLog.stdout)
	}
}

func TestExplainCommand(t *testing.T) {
	res := runBootlore(t, "", "explain", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "  The bootloader loaded the kernel image into memory") {
		t.Errorf("stdout should contain the indented technical explanation for the kernel line:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "[failure] SYSTEMD: A unit reported a failure.") {
		t.Errorf("stdout should contain the interpreted failure line:\n%s", res.stdout)
	}
}

func TestExplainLevelOverview(t *testing.T) {
	res := runBootlore(t, "", "explain", "--level", "overview", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "  This is one of the very first boot messages") {
		t.Errorf("stdout should contain the overview-level explanation:\n%s", res.stdout)
	}
}

func TestExplainBadLevelFails(t *testing.T) {
	res := runBootlore(t, "", "explain", "--level", "bogus", "--input", fixturePath)
	if res.code != 2 {
		t.Errorf("exit code = %d, want 2", res.code)
	}
}

// The timeline must place every fixture event at its monotonic offset since
// boot, in chronological order, in the HUMAN view.
func TestTimelineCommand(t *testing.T) {
	res := runBootlore(t, "", "timeline", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	lines := nonEmptyLines(res.stdout)
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5:\n%s", len(lines), res.stdout)
	}
	if !strings.Contains(lines[0], "+1.200s [normal] KERNEL: The Linux kernel is starting and reports its version.") {
		t.Errorf("first line = %q", lines[0])
	}
	if !strings.Contains(lines[4], "+6.000s [failure] SYSTEMD: A unit reported a failure.") {
		t.Errorf("last line = %q", lines[4])
	}
	// Offsets must be printed in ascending order.
	if i, j := strings.Index(res.stdout, "+1.200s"), strings.Index(res.stdout, "+6.000s"); i > j {
		t.Errorf("timeline is not chronological: +1.200s at %d, +6.000s at %d", i, j)
	}
}

// --category must narrow every reading command, end to end on the shared
// fixture (kernel x2 -> one is storage, systemd, network, a systemd failure).
func TestCategoryFilterEndToEnd(t *testing.T) {
	res := runBootlore(t, "", "collect", "--category", "network", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	lines := nonEmptyLines(res.stdout)
	if len(lines) != 1 || !strings.Contains(lines[0], "Started Network Manager.") {
		t.Errorf("--category network should keep only the NetworkManager line:\n%s", res.stdout)
	}

	res = runBootlore(t, "", "timeline", "--category", "kernel,storage", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if got := len(nonEmptyLines(res.stdout)); got != 2 {
		t.Errorf("--category kernel,storage should keep 2 timeline lines, got %d:\n%s", got, res.stdout)
	}

	res = runBootlore(t, "", "errors", "--category", "network", "--input", fixturePath)
	if !strings.Contains(res.stdout, "No warnings or failures found.") {
		t.Errorf("no network failure in the fixture:\n%s", res.stdout)
	}
}

// On a user-session journal (every entry hosted by user@1000.service) the
// subsystems must still be told apart, end to end through the real binary.
func TestCategoryFilterOnUserSessionJournal(t *testing.T) {
	session := filepath.Join(moduleRoot, "test", "testdata", "user-session-journal.json")

	res := runBootlore(t, "", "collect", "--category", "desktop", "--input", session)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if got := len(nonEmptyLines(res.stdout)); got != 3 {
		t.Errorf("--category desktop should keep the 3 desktop events (audio session, portal, keyring), got %d:\n%s", got, res.stdout)
	}

	res = runBootlore(t, "", "collect", "--category", "unclassified", "--input", session)
	lines := nonEmptyLines(res.stdout)
	if len(lines) != 1 || !strings.Contains(lines[0], "TensorFlow Lite") {
		t.Errorf("--category unclassified should show only the unknown application:\n%s", res.stdout)
	}

	// The portal failure is a desktop failure, not a systemd one.
	res = runBootlore(t, "", "errors", "--category", "desktop", "--input", session)
	if !strings.Contains(res.stdout, "[failure] DESKTOP:") {
		t.Errorf("errors --category desktop should report the portal failure:\n%s", res.stdout)
	}
}

// Without a terminal the TUI must fail cleanly, not hang or spew escapes.
func TestTuiRequiresTerminalEndToEnd(t *testing.T) {
	res := runBootlore(t, "", "tui")
	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "terminal") {
		t.Errorf("stderr should explain the terminal requirement: %q", res.stderr)
	}
	if strings.Contains(res.stdout, "\x1b") {
		t.Error("no escape sequences may leak to a non-tty stdout")
	}

	res = runBootlore(t, "", "tui", "--category", "bogus")
	if res.code != 2 {
		t.Errorf("bad category: exit code = %d, want 2", res.code)
	}
}

func TestCategoryInvalidEndToEnd(t *testing.T) {
	res := runBootlore(t, "", "collect", "--category", "bogus", "--input", fixturePath)
	if res.code != 2 {
		t.Fatalf("exit code = %d, want 2", res.code)
	}
	if !strings.Contains(res.stderr, "valid:") {
		t.Errorf("stderr should list the valid categories: %q", res.stderr)
	}
}

func TestTimelineViewLog(t *testing.T) {
	res := runBootlore(t, "", "timeline", "--view", "log", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "+4.800s [info] NetworkManager[412]: Started Network Manager.") {
		t.Errorf("log timeline should show original messages with offsets:\n%s", res.stdout)
	}
}

// --- errors command / live view (end-to-end) ---

func TestErrorsCommand(t *testing.T) {
	res := runBootlore(t, "", "errors", "--input", fixturePath)
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	lines := nonEmptyLines(res.stdout)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (only the bluetooth failure is noteworthy):\n%s", len(lines), res.stdout)
	}
	if !strings.Contains(lines[0], "[failure] SYSTEMD: A unit reported a failure.") {
		t.Errorf("line = %q", lines[0])
	}
}

func TestErrorsNoneFound(t *testing.T) {
	normal := `{"__REALTIME_TIMESTAMP":"1700000000000000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","MESSAGE":"Reached target Multi-User System."}`
	res := runBootlore(t, normal, "errors")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "No warnings or failures found.") {
		t.Errorf("stdout should say that nothing noteworthy was found:\n%s", res.stdout)
	}
}

func TestErrorsJournalBackendMissing(t *testing.T) {
	res := runBootloreEnv(t, []string{"PATH=" + t.TempDir()}, "", "errors", "--journal")
	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "journalctl") {
		t.Errorf("stderr should mention journalctl: %q", res.stderr)
	}
}

func TestExplainJournalBackendMissing(t *testing.T) {
	res := runBootloreEnv(t, []string{"PATH=" + t.TempDir()}, "", "explain", "--journal")
	if res.code != 1 {
		t.Fatalf("exit code = %d, want 1 (stderr = %q)", res.code, res.stderr)
	}
	if !strings.Contains(res.stderr, "journalctl") {
		t.Errorf("stderr should mention journalctl: %q", res.stderr)
	}
}

func TestLiveViewHuman(t *testing.T) {
	dir := collectortest.WriteStubJournalctl(t, collectortest.StubReplayThenLive)
	res := runBootloreEnv(t, []string{"PATH=" + dir}, "", "live", "--view", "human")
	if res.code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "[normal] replayed 2") {
		t.Errorf("stdout should contain the human-view replay lines:\n%s", res.stdout)
	}
	if !strings.Contains(res.stdout, "[normal] live event") {
		t.Errorf("stdout should contain the human-view live line:\n%s", res.stdout)
	}
	if strings.Index(res.stdout, "replayed 2") > strings.Index(res.stdout, "live event") {
		t.Error("replayed events must be printed before live events")
	}
}
