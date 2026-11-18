//go:build system

// Package system contains end-to-end tests that build the real bootlore binary
// and run it as a separate process, exercising the full CLI the way a user
// would.
//
// Run with: go test -tags=system ./test/system/...
package system

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
	if !strings.Contains(res.stdout, "[normal] NETWORK: A systemd unit changed state as part of the boot sequence.") {
		t.Errorf("stdout should contain the interpreted NetworkManager line:\n%s", res.stdout)
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
