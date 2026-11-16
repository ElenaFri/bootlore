// Package collector retrieves boot events from the system journal and turns them
// into BootLore's normalized event model.
//
// The initial implementation reads boot sessions from systemd's journal by
// invoking journalctl in JSON mode. Collection is read-only and does not require
// root: it only sees what the invoking user is allowed to read.
//
// The actual journal backend is abstracted behind the Runner interface so that
// the collection logic can be unit-tested with a fake, while the real
// implementation (NewExecRunner) shells out to journalctl.
//
// Three collection modes are provided:
//
//   - Collect:       one-shot historical replay of a boot.
//   - Follow:        live monitoring of new events as they arrive.
//   - Watch:         seamless replay followed by live monitoring. The replay
//     remembers the journal cursor of its last event and the live phase resumes
//     strictly after that cursor (journalctl --after-cursor), so the
//     transition neither duplicates nor misses events.
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
	// Follow enables live monitoring (journalctl -f): the stream does not end
	// on its own and must be stopped by canceling the context.
	Follow bool
	// AfterCursor resumes strictly after this journal cursor
	// (journalctl --after-cursor). Empty means "from the beginning".
	AfterCursor string
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
	args := []string{"-o", "json", "--no-pager", "-b", boot}
	if opt.AfterCursor != "" {
		args = append(args, "--after-cursor="+opt.AfterCursor)
	}
	if opt.Follow {
		args = append(args, "-f")
	}
	return args
}

// Collect retrieves the requested boot's events as a one-shot historical replay.
//
// Events are tagged with event.OriginHistorical: they are reconstructed from the
// journal, not observed live by BootLore.
func (c *Collector) Collect(ctx context.Context, opt Options) ([]event.Event, error) {
	var events []event.Event
	err := c.CollectStream(ctx, opt, func(e event.Event) error {
		events = append(events, e)
		return nil
	})
	return events, err
}

// CollectStream is the streaming counterpart of Collect: it delivers each
// historical event to fn as soon as it is parsed.
func (c *Collector) CollectStream(ctx context.Context, opt Options, fn func(event.Event) error) error {
	return c.stream(ctx, opt, event.OriginHistorical, fn)
}

// Follow monitors the journal live and delivers each new event to fn as it
// arrives, tagged with event.OriginLive. It blocks until the stream ends or
// ctx is canceled; cancellation (e.g. Ctrl-C) is a normal stop and returns nil.
func (c *Collector) Follow(ctx context.Context, opt Options, fn func(event.Event) error) error {
	opt.Follow = true
	err := c.stream(ctx, opt, event.OriginLive, fn)
	if err != nil && ctx.Err() != nil {
		// The context cancellation killed the backend; that is the normal way
		// to stop live monitoring, not a failure.
		return nil
	}
	return err
}

// Watch performs a seamless historical replay of the requested boot followed by
// live monitoring, delivering every event exactly once.
//
// The replay phase remembers the journal cursor of the last event it delivered;
// the live phase then resumes strictly after that cursor, so no event is
// duplicated or missed across the transition. Historical events are tagged
// event.OriginHistorical, live ones event.OriginLive. If the journal contains
// nothing yet, the live phase falls back to a plain follow.
//
// Watch blocks until ctx is canceled or the live stream ends on its own.
func (c *Collector) Watch(ctx context.Context, opt Options, fn func(event.Event) error) error {
	var lastCursor string
	err := c.CollectStream(ctx, opt, func(e event.Event) error {
		if e.Cursor != "" {
			lastCursor = e.Cursor
		}
		return fn(e)
	})
	if err != nil || ctx.Err() != nil {
		// Cancellation during the replay is a normal stop, like in Follow.
		if ctx.Err() != nil {
			return nil
		}
		return err
	}

	return c.Follow(ctx, Options{Boot: opt.Boot, AfterCursor: lastCursor}, fn)
}

// stream runs the backend once and pumps parsed events to fn with the given
// origin. The runner's reader is closed after parsing; a backend that failed
// (e.g. missing permissions) surfaces through that close.
func (c *Collector) stream(ctx context.Context, opt Options, origin event.Origin, fn func(event.Event) error) error {
	rc, err := c.runner.Run(ctx, JournalArgs(opt))
	if err != nil {
		return fmt.Errorf("collector: reading journal: %w", err)
	}

	streamErr := journal.ParseStream(rc, origin, fn)
	if streamErr != nil {
		rc.Close()
		return fmt.Errorf("collector: %w", streamErr)
	}
	if err := rc.Close(); err != nil {
		return fmt.Errorf("collector: reading journal: %w", err)
	}
	return nil
}
