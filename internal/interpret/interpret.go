// Package interpret is BootLore's event interpreter (architecture §4.5): it
// combines classification, operational significance and the knowledge base to
// fill an event's interpretation fields.
//
// Interpret fills, on a copy of the event:
//
//   - Category, via the classify package;
//   - Significance, via the significance package;
//   - Human, from the matching knowledge entry's summary;
//   - Explanation, from the entry's technical level (the default depth for the
//     embedded explanation; the EXPLAIN view can fetch other levels from the
//     knowledge base directly).
//
// Interpretation is idempotent and never overwrites existing values, and source
// data is never modified. Events with no reliable interpretation keep their
// empty fields; the presentation layer falls back to the original message.
package interpret

import (
	"github.com/ElenaFri/bootlore/internal/classify"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/knowledge"
	"github.com/ElenaFri/bootlore/internal/significance"
)

// Interpret returns a copy of e enriched with BootLore's interpretation.
func Interpret(e event.Event) event.Event {
	e = classify.ClassifyEvent(e)
	e = significance.AssessEvent(e)

	if e.Human == "" || e.Explanation == "" {
		entry, ok := knowledge.For(e)
		if ok {
			if e.Human == "" {
				e.Human = entry.Summary
			}
			if e.Explanation == "" {
				e.Explanation = entry.Text(knowledge.LevelTechnical)
			}
		}
	}
	return e
}
