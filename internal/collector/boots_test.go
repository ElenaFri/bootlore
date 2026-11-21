package collector_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
)

func TestBootArgs(t *testing.T) {
	want := []string{"--no-pager", "--list-boots"}
	if got := collector.BootArgs(); !reflect.DeepEqual(got, want) {
		t.Errorf("BootArgs() = %v, want %v", got, want)
	}
}

func TestBootCountCountsListBootsLines(t *testing.T) {
	fr := &collectortest.FakeRunner{Output: ` -5 ab288f97 Sun 2026-11-15 20:30:38 CET Sun 2026-11-15 20:31:03 CET
 -4 ca4b754b Sun 2026-11-15 20:31:24 CET Sun 2026-11-15 22:30:55 CET
 -3 52fdb7f3 Sun 2026-11-15 22:32:03 CET Sun 2026-11-15 22:44:06 CET
  0 48be9b9b Fri 2026-11-20 11:01:03 CET Fri 2026-11-20 11:23:01 CET
`}
	c := collector.New(fr)

	n, err := c.BootCount(context.Background())
	if err != nil {
		t.Fatalf("BootCount returned error: %v", err)
	}
	if n != 4 {
		t.Errorf("BootCount = %d, want 4", n)
	}
	want := []string{"--no-pager", "--list-boots"}
	if !reflect.DeepEqual(fr.LastArgs(), want) {
		t.Errorf("runner args = %v, want %v", fr.LastArgs(), want)
	}
	if !fr.Closed() {
		t.Error("BootCount should close the runner's reader")
	}
}

func TestBootCountZeroOnEmptyJournal(t *testing.T) {
	// An empty or rotated journal yields no list-boots lines on stdout
	// (diagnostics go to stderr and are not part of the counted stream).
	fr := &collectortest.FakeRunner{Output: ""}
	c := collector.New(fr)

	n, err := c.BootCount(context.Background())
	if err != nil {
		t.Fatalf("BootCount returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("BootCount = %d, want 0", n)
	}
}

func TestBootCountPropagatesRunnerError(t *testing.T) {
	fr := &collectortest.FakeRunner{Err: errors.New("boom")}
	c := collector.New(fr)

	if _, err := c.BootCount(context.Background()); err == nil {
		t.Fatal("BootCount should report the runner error")
	} else if !strings.Contains(err.Error(), "journal boots") {
		t.Errorf("error = %v, want it to mention journal boots", err)
	}
}
