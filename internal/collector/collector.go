// Package collector retrieves boot events from the system journal and turns them
// into BootLore's normalized event model.
//
// The initial implementation reads the current boot session from systemd's
// journal by invoking journalctl in JSON mode. Collection is read-only and does
// not require root: it only sees what the invoking user is allowed to read.
//
// The actual journal backend is abstracted behind the Runner interface so that
// the collection logic can be unit-tested with a fake, while the real
// implementation (NewExecRunner) shells out to journalctl.
package collector

import (
	"context"
	"fmt"
	"io"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/journal"
)

// Options controls what the collector retrieves.
type Options struct {
	// Boot selects which boot to read, as a journalctl -b offset
	// ("0" or "" for the current boot, "-1" for the previous one, ...).
	Boot string
}

// Runner executes the journal backend with the given arguments and returns a
// stream of its output. Callers must Close the returned reader.
type Runner interface {
	Run(ctx context.Context, args []string) (io.ReadCloser, error)
}

// Collector reads events from a journal Runner and normalizes them.
type Collector struct {
	runner Runner
}

// New returns a Collector backed by the given Runner.
func New(r Runner) *Collector {
	return &Collector{runner: r}
}

// JournalArgs builds the journalctl arguments for the given options. It always
// requests JSON output with paging disabled, scoped to a single boot.
func JournalArgs(opt Options) []string {
	boot := opt.Boot
	if boot == "" {
		boot = "0" // current boot
	}
	return []string{"-o", "json", "--no-pager", "-b", boot}
}

// Collect retrieves the requested boot's events as a historical replay.
//
// Events are tagged with event.OriginHistorical: they are reconstructed from the
// journal, not observed live by BootLore. Live monitoring is a separate concern.
func (c *Collector) Collect(ctx context.Context, opt Options) ([]event.Event, error) {
	rc, err := c.runner.Run(ctx, JournalArgs(opt))
	if err != nil {
		return nil, fmt.Errorf("collector: reading journal: %w", err)
	}
	defer rc.Close()

	events, err := journal.ParseWithOrigin(rc, event.OriginHistorical)
	if err != nil {
		return nil, fmt.Errorf("collector: %w", err)
	}
	return events, nil
}
