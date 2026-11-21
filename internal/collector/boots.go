package collector

import (
	"bufio"
	"context"
	"fmt"
)

// BootArgs builds the journalctl arguments that list the recorded boots.
func BootArgs() []string {
	return []string{"--no-pager", "--list-boots"}
}

// BootCount reports how many boots the journal has recorded, by counting the
// non-empty lines of journalctl --list-boots. It only sees the boots the
// invoking user is allowed to read.
func (c *Collector) BootCount(ctx context.Context) (int, error) {
	rc, err := c.runner.Run(ctx, BootArgs())
	if err != nil {
		return 0, fmt.Errorf("journal boots: %w", err)
	}
	defer rc.Close()

	n := 0
	sc := bufio.NewScanner(rc)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			n++
		}
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("journal boots: %w", err)
	}
	return n, nil
}
