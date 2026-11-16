package collector

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunErrorSurfacesBackendStderr(t *testing.T) {
	err := runError("journalctl", errors.New("fork failed"), "some backend complaint")
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("error should wrap ErrBackendUnavailable, got %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "journalctl") {
		t.Errorf("error should name the backend: %q", msg)
	}
	if !strings.Contains(msg, "some backend complaint") {
		t.Errorf("error should surface backend stderr: %q", msg)
	}
}

func TestRunErrorWithoutStderr(t *testing.T) {
	err := runError("journalctl", errors.New("fork failed"), "")
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Errorf("error should wrap ErrBackendUnavailable, got %v", err)
	}
	if !strings.Contains(err.Error(), "running journalctl") {
		t.Errorf("error should describe the failure: %q", err.Error())
	}
}

// A missing journalctl must fail fast with ErrBackendUnavailable, without
// spawning any process.
func TestExecRunnerRunBinaryMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: journalctl is nowhere to be found
	r := NewExecRunner()
	_, err := r.Run(context.Background(), JournalArgs(Options{}))
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("error = %v, want ErrBackendUnavailable", err)
	}
	if !strings.Contains(err.Error(), "journalctl") {
		t.Errorf("error should mention journalctl: %q", err.Error())
	}
}
