// Package filter selects events by the subsystem category BootLore assigned to
// them, so users can focus a view on, for example, network or storage events.
//
// A Filter is built from a user-facing specification ("network,storage") and
// applied to events that have already been interpreted (their Category is
// filled). The zero value filters nothing.
package filter

import (
	"fmt"
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
)

// categoryNames lists the user-facing category names in display order. The
// special name "unclassified" selects the events BootLore could not classify,
// which is useful to see what the classifier does not cover.
var categoryNames = []struct {
	name string
	cat  event.Category
}{
	{"kernel", event.CategoryKernel},
	{"hardware", event.CategoryHardware},
	{"storage", event.CategoryStorage},
	{"network", event.CategoryNetwork},
	{"systemd", event.CategorySystemd},
	{"security", event.CategorySecurity},
	{"desktop", event.CategoryDesktop},
	{"other", event.CategoryOther},
	{"unclassified", event.CategoryUnclassified},
}

// Names returns the valid user-facing category names, in display order.
func Names() []string {
	out := make([]string, len(categoryNames))
	for i, c := range categoryNames {
		out[i] = c.name
	}
	return out
}

// Filter selects events by category. The zero value matches every event.
type Filter struct {
	categories map[event.Category]bool
}

// ParseCategories builds a Filter from a comma-separated list of category names
// (case-insensitive, surrounding spaces ignored). An empty specification yields
// an empty filter that matches everything. Unknown names are an error listing
// the valid ones.
func ParseCategories(spec string) (Filter, error) {
	if strings.TrimSpace(spec) == "" {
		return Filter{}, nil
	}

	set := make(map[event.Category]bool)
	for _, raw := range strings.Split(spec, ",") {
		name := strings.ToLower(strings.TrimSpace(raw))
		cat, ok := lookup(name)
		if !ok {
			return Filter{}, fmt.Errorf("unknown category %q (valid: %s)", raw, strings.Join(Names(), ", "))
		}
		set[cat] = true
	}
	return Filter{categories: set}, nil
}

// Empty reports whether the filter selects everything.
func (f Filter) Empty() bool {
	return len(f.categories) == 0
}

// Match reports whether e passes the filter. An empty filter matches every
// event.
func (f Filter) Match(e event.Event) bool {
	if f.Empty() {
		return true
	}
	return f.categories[e.Category]
}

func lookup(name string) (event.Category, bool) {
	for _, c := range categoryNames {
		if c.name == name {
			return c.cat, true
		}
	}
	return event.CategoryUnclassified, false
}
