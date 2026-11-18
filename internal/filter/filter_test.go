package filter

import (
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
)

func TestParseCategoriesValid(t *testing.T) {
	cases := map[string][]event.Category{
		"network":                 {event.CategoryNetwork},
		"NETWORK":                 {event.CategoryNetwork},
		"Kernel":                  {event.CategoryKernel},
		"network,storage":         {event.CategoryNetwork, event.CategoryStorage},
		" network , storage ":     {event.CategoryNetwork, event.CategoryStorage},
		"hardware,security,other": {event.CategoryHardware, event.CategorySecurity, event.CategoryOther},
		"systemd,desktop":         {event.CategorySystemd, event.CategoryDesktop},
		"unclassified":            {event.CategoryUnclassified},
		"network,network":         {event.CategoryNetwork},
	}
	for in, want := range cases {
		f, err := ParseCategories(in)
		if err != nil {
			t.Errorf("ParseCategories(%q) returned unexpected error: %v", in, err)
			continue
		}
		if f.Empty() {
			t.Errorf("ParseCategories(%q) should not be an empty filter", in)
		}
		for _, c := range want {
			if !f.Match(event.Event{Category: c}) {
				t.Errorf("ParseCategories(%q) should match category %q", in, c)
			}
		}
	}
}

// An empty specification means "no filtering": everything passes.
func TestParseCategoriesEmptyMatchesEverything(t *testing.T) {
	for _, in := range []string{"", "   "} {
		f, err := ParseCategories(in)
		if err != nil {
			t.Fatalf("ParseCategories(%q) returned unexpected error: %v", in, err)
		}
		if !f.Empty() {
			t.Errorf("ParseCategories(%q) should be empty", in)
		}
		for _, c := range []event.Category{event.CategoryKernel, event.CategoryUnclassified, event.CategoryOther} {
			if !f.Match(event.Event{Category: c}) {
				t.Errorf("empty filter should match category %q", c)
			}
		}
	}
	var zero Filter
	if !zero.Empty() || !zero.Match(event.Event{Category: event.CategoryNetwork}) {
		t.Error("the zero-value Filter should match everything")
	}
}

// Unknown names are rejected with an error that tells the user what is valid.
func TestParseCategoriesUnknownIsAnError(t *testing.T) {
	for _, in := range []string{"bogus", "network,bogus", "net work", "network;storage", ","} {
		_, err := ParseCategories(in)
		if err == nil {
			t.Errorf("ParseCategories(%q) should return an error", in)
			continue
		}
		if !strings.Contains(err.Error(), "network") {
			t.Errorf("error for %q should list the valid categories, got: %v", in, err)
		}
	}
}

func TestMatchSelectsOnlyRequestedCategories(t *testing.T) {
	f, err := ParseCategories("network,storage")
	if err != nil {
		t.Fatal(err)
	}
	matching := []event.Category{event.CategoryNetwork, event.CategoryStorage}
	for _, c := range matching {
		if !f.Match(event.Event{Category: c}) {
			t.Errorf("filter should match %q", c)
		}
	}
	rejected := []event.Category{
		event.CategoryKernel, event.CategorySystemd, event.CategorySecurity,
		event.CategoryDesktop, event.CategoryHardware, event.CategoryOther, event.CategoryUnclassified,
	}
	for _, c := range rejected {
		if f.Match(event.Event{Category: c}) {
			t.Errorf("filter should reject %q", c)
		}
	}
}

// "unclassified" lets users inspect exactly what BootLore could not classify.
func TestMatchUnclassifiedOnly(t *testing.T) {
	f, err := ParseCategories("unclassified")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Match(event.Event{}) {
		t.Error("filter should match an unclassified event")
	}
	if f.Match(event.Event{Category: event.CategoryKernel}) {
		t.Error("filter should reject a classified event")
	}
}

func TestNamesListsEveryCategory(t *testing.T) {
	names := Names()
	want := []string{"kernel", "hardware", "storage", "network", "systemd", "security", "desktop", "other", "unclassified"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("Names() = %v, want %v", names, want)
	}
}
