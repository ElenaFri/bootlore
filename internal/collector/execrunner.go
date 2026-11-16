package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// DefaultBinary is the journal backend BootLore invokes.
const DefaultBinary = "journalctl"

// ErrBackendUnavailable indicates the journal backend (journalctl) could not be
// found or executed. Callers can use errors.Is to detect it and advise the user.
var ErrBackendUnavailable = errors.New("journal backend unavailable")

// execRunner runs the real journalctl binary and buffers its output.
//
// Buffering (rather than streaming) is sufficient for historical replay of a
// single boot; live following will warrant a streaming runner later.
type execRunner struct {
	binary string
}

// NewExecRunner returns a Runner that executes the system journalctl binary.
func NewExecRunner() Runner {
	return &execRunner{binary: DefaultBinary}
}

func (r *execRunner) Run(ctx context.Context, args []string) (io.ReadCloser, error) {
	if _, err := exec.LookPath(r.binary); err != nil {
		return nil, fmt.Errorf(
			"%w: %q not found in PATH (BootLore needs systemd's journalctl to read the journal)",
			ErrBackendUnavailable, r.binary)
	}

	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		return nil, runError(r.binary, err, stderr.String())
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

// runError turns an exec failure into a clear, actionable error, surfacing the
// backend's own stderr (e.g. permission problems) when available.
func runError(binary string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if msg == "" {
			msg = exitErr.String()
		}
		return fmt.Errorf("%w: %s failed: %s", ErrBackendUnavailable, binary, msg)
	}

	if msg != "" {
		return fmt.Errorf("%w: %s: %v: %s", ErrBackendUnavailable, binary, err, msg)
	}
	return fmt.Errorf("%w: running %s: %v", ErrBackendUnavailable, binary, err)
}
