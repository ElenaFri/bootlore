// Package journal converts the JSON output of systemd's journalctl into
// BootLore's normalized event model.
//
// It accepts the newline-delimited JSON produced by `journalctl -o json`
// (one object per entry) and maps the well-known journal fields onto
// event.Event. Fields that are not promoted to dedicated struct fields are kept
// in Event.Fields so that no original information is lost during normalization.
//
// Parsing is deliberately lenient about individual field contents (a missing or
// invalid PRIORITY becomes PriorityUnknown rather than an error) but strict
// about structure: a line that is not a valid JSON object aborts parsing, since
// that usually signals a truncated or corrupted stream.
package journal

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// Journal field names promoted onto dedicated Event fields.
const (
	fieldMessage   = "MESSAGE"
	fieldRealtime  = "__REALTIME_TIMESTAMP"
	fieldMonotonic = "__MONOTONIC_TIMESTAMP"
	fieldBootID    = "_BOOT_ID"
	fieldCursor    = "__CURSOR"
	fieldPriority  = "PRIORITY"
	fieldPID       = "_PID"
	fieldUnit      = "_SYSTEMD_UNIT"
	fieldSyslogID  = "SYSLOG_IDENTIFIER"
	fieldComm      = "_COMM"
)

// promoted lists the fields that are mapped onto dedicated Event fields and must
// therefore not be duplicated into Event.Fields.
var promoted = map[string]struct{}{
	fieldMessage:   {},
	fieldRealtime:  {},
	fieldMonotonic: {},
	fieldBootID:    {},
	fieldCursor:    {},
	fieldPriority:  {},
	fieldPID:       {},
	fieldUnit:      {},
	fieldSyslogID:  {},
	fieldComm:      {},
}

// Parse reads newline-delimited journal JSON from r and returns the normalized
// events in the order they appear. The collection origin is left unset; use
// ParseWithOrigin to record how the events were observed.
func Parse(r io.Reader) ([]event.Event, error) {
	return ParseWithOrigin(r, event.OriginUnknown)
}

// ParseWithOrigin behaves like Parse but tags every produced event with the
// given collection origin (historical replay or live monitoring).
func ParseWithOrigin(r io.Reader, origin event.Origin) ([]event.Event, error) {
	var events []event.Event
	err := ParseStream(r, origin, func(e event.Event) error {
		events = append(events, e)
		return nil
	})
	return events, err
}

// ParseStream is the streaming counterpart of ParseWithOrigin: it decodes
// entries one by one and delivers each event to fn as soon as it is parsed,
// without waiting for the stream to end. This is what makes live monitoring
// possible.
//
// fn's error stops the stream immediately and is returned as-is (not wrapped),
// so callers may use sentinel errors to stop early. Parsing errors are
// wrapped with the entry number.
func ParseStream(r io.Reader, origin event.Origin, fn func(event.Event) error) error {
	dec := json.NewDecoder(r)
	for n := 1; ; n++ {
		var raw map[string]json.RawMessage
		err := dec.Decode(&raw)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("journal: decoding entry %d: %w", n, err)
		}
		e, err := entryToEvent(raw)
		if err != nil {
			return fmt.Errorf("journal: entry %d: %w", n, err)
		}
		e.Origin = origin
		if err := fn(e); err != nil {
			return err
		}
	}
}

func entryToEvent(raw map[string]json.RawMessage) (event.Event, error) {
	e := event.Event{Priority: event.PriorityUnknown}

	if v, ok := raw[fieldMessage]; ok {
		msg, err := decodeValue(v)
		if err != nil {
			return event.Event{}, fmt.Errorf("decoding MESSAGE: %w", err)
		}
		e.Message = msg
	}

	e.Source = firstString(raw, fieldSyslogID, fieldComm)
	e.Unit = stringField(raw, fieldUnit)
	e.BootID = stringField(raw, fieldBootID)
	e.Cursor = stringField(raw, fieldCursor)

	if s := stringField(raw, fieldPriority); s != "" {
		// Invalid priorities are tolerated and left as PriorityUnknown.
		if p, err := event.ParsePriority(s); err == nil {
			e.Priority = p
		}
	}

	if s := stringField(raw, fieldPID); s != "" {
		if pid, err := strconv.Atoi(s); err == nil {
			e.PID = pid
		}
	}

	if s := stringField(raw, fieldRealtime); s != "" {
		if micros, err := strconv.ParseInt(s, 10, 64); err == nil {
			e.RealtimeTimestamp = time.Unix(0, micros*int64(time.Microsecond)).UTC()
		}
	}

	if s := stringField(raw, fieldMonotonic); s != "" {
		if micros, err := strconv.ParseInt(s, 10, 64); err == nil {
			e.MonotonicTimestamp = time.Duration(micros) * time.Microsecond
		}
	}

	// Preserve every remaining field so no original information is lost.
	for k, v := range raw {
		if _, skip := promoted[k]; skip {
			continue
		}
		s, err := decodeValue(v)
		if err != nil {
			continue
		}
		if e.Fields == nil {
			e.Fields = make(map[string]string)
		}
		e.Fields[k] = s
	}

	return e, nil
}

// stringField returns the value of a field as a string, or "" if absent or not
// decodable as a string-like value.
func stringField(raw map[string]json.RawMessage, key string) string {
	v, ok := raw[key]
	if !ok {
		return ""
	}
	s, err := decodeValue(v)
	if err != nil {
		return ""
	}
	return s
}

// firstString returns the first non-empty string field among keys.
func firstString(raw map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		if s := stringField(raw, k); s != "" {
			return s
		}
	}
	return ""
}

// decodeValue turns a journal field value into a string. journald encodes most
// values as JSON strings, but non-UTF-8 data (and some fields) are encoded as an
// array of byte values. Arrays of strings (repeated fields) are joined with
// newlines.
func decodeValue(raw json.RawMessage) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, nil
	}

	var nums []int
	if err := json.Unmarshal(raw, &nums); err == nil {
		b := make([]byte, len(nums))
		for i, n := range nums {
			b[i] = byte(n)
		}
		return string(b), nil
	}

	var strs []string
	if err := json.Unmarshal(raw, &strs); err == nil {
		out := ""
		for i, v := range strs {
			if i > 0 {
				out += "\n"
			}
			out += v
		}
		return out, nil
	}

	return "", fmt.Errorf("unsupported value %s", string(raw))
}
