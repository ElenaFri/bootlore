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

// execRunner runs the real journalctl binary and streams its output.
//
// Streaming (rather than buffering) is required for live monitoring: with
// journalctl -f the process runs until it is killed, so its output must be
// consumed while it is still running. For historical replay the stream simply
// ends when journalctl exits.
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

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, runError(r.binary, err, stderr.String())
	}
	if err := cmd.Start(); err != nil {
		return nil, runError(r.binary, err, stderr.String())
	}
	return &cmdStream{cmd: cmd, stdout: stdout, stderr: &stderr}, nil
}

// cmdStream adapts a started command's stdout pipe into an io.ReadCloser.
// Read streams the backend's output live; Close reaps the process and turns a
// backend failure (non-zero exit, e.g. a permission problem) into an error.
type cmdStream struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
}

func (s *cmdStream) Read(p []byte) (int, error) {
	return s.stdout.Read(p)
}

func (s *cmdStream) Close() error {
	s.stdout.Close()
	return runError(s.cmd.Args[0], s.cmd.Wait(), s.stderr.String())
}

// runError turns an exec failure into a clear, actionable error, surfacing the
// backend's own stderr (e.g. permission problems) when available. A nil err is
// passed through as nil.
func runError(binary string, err error, stderr string) error {
	if err == nil {
		return nil
	}

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
