package main

import (
	"context"
	"io"
	"strings"
)

// doctorRunner is a collector.Runner double for the doctor command. It answers
// the journalctl boot listing and the boot event stream separately, so a
// single doctor run can be exercised end to end.
type doctorRunner struct {
	Boots   string
	Events  string
	Err     error
	gotRuns [][]string
}

func (d *doctorRunner) Run(_ context.Context, args []string) (io.ReadCloser, error) {
	d.gotRuns = append(d.gotRuns, args)
	if d.Err != nil {
		return nil, d.Err
	}
	if strings.Contains(strings.Join(args, " "), "--list-boots") {
		return io.NopCloser(strings.NewReader(d.Boots)), nil
	}
	return io.NopCloser(strings.NewReader(d.Events)), nil
}

// GotRuns returns the arguments of every Run call, in order.
func (d *doctorRunner) GotRuns() [][]string { return d.gotRuns }
