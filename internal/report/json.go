package report

import (
	"encoding/json"

	"github.com/ElenaFri/bootlore/internal/render"
	"github.com/ElenaFri/bootlore/internal/summary"
)

// The json* types define BootLore's stable, documented report schema. They are
// decoupled from the internal analysis structs on purpose: the JSON contract can
// stay stable even if the internals change, and durations are rendered in the
// same human form the text report uses ("5.000s", "+1.200s") so the two views
// never disagree.

type jsonReport struct {
	Tool      string       `json:"tool"`
	Version   string       `json:"version,omitempty"`
	Generated string       `json:"generated,omitempty"`
	Boot      jsonBoot     `json:"boot"`
	Timing    jsonTiming   `json:"timing"`
	Services  jsonServices `json:"services"`
}

type jsonBoot struct {
	ID         string         `json:"id"`
	Boots      int            `json:"boots"`
	Events     int            `json:"events"`
	Normal     int            `json:"normal"`
	Warnings   int            `json:"warnings"`
	Failures   int            `json:"failures"`
	Unassessed int            `json:"unassessed"`
	First      string         `json:"first,omitempty"`
	Last       string         `json:"last,omitempty"`
	Span       string         `json:"span,omitempty"`
	IntoBoot   string         `json:"into_boot,omitempty"`
	Subsystems map[string]int `json:"subsystems"`
}

type jsonTiming struct {
	Total      string          `json:"total"`
	Source     string          `json:"source"`
	Phases     []jsonPhase     `json:"phases"`
	Milestones []jsonMilestone `json:"milestones"`
}

type jsonPhase struct {
	Name     string `json:"name"`
	Duration string `json:"duration"`
}

type jsonMilestone struct {
	Name string `json:"name"`
	At   string `json:"at"`
}

type jsonServices struct {
	Failed []jsonFailure `json:"failed"`
	Slow   []jsonSlow    `json:"slow"`
}

type jsonFailure struct {
	Unit    string `json:"unit"`
	Message string `json:"message"`
	At      string `json:"at"`
	Count   int    `json:"count"`
}

type jsonSlow struct {
	Name     string `json:"name"`
	Duration string `json:"duration"`
	Started  string `json:"started"`
}

// FormatJSON renders the report as indented JSON following BootLore's report
// schema. The output has no trailing newline.
func FormatJSON(r Report) (string, error) {
	out, err := json.MarshalIndent(toJSON(r), "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// toJSON projects a Report onto its stable JSON schema.
func toJSON(r Report) jsonReport {
	jr := jsonReport{
		Tool:     tool(r.Meta),
		Version:  r.Meta.Version,
		Boot:     bootJSON(r),
		Timing:   timingJSON(r),
		Services: servicesJSON(r),
	}
	if !r.Meta.Generated.IsZero() {
		jr.Generated = utc(r.Meta.Generated)
	}
	return jr
}

// tool returns the provenance tool name, defaulting to "bootlore".
func tool(m Meta) string {
	if m.Tool == "" {
		return "bootlore"
	}
	return m.Tool
}

func bootJSON(r Report) jsonBoot {
	s := r.Summary
	b := jsonBoot{
		ID:         summary.BootLabel(s),
		Boots:      s.Boots,
		Events:     s.Total,
		Normal:     s.Normal,
		Warnings:   s.Warnings,
		Failures:   s.Failures,
		Unassessed: s.Unassessed,
		Subsystems: map[string]int{},
	}
	if !s.First.IsZero() {
		b.First = utc(s.First)
		b.Last = utc(s.Last)
		b.Span = render.Duration(s.Span)
	}
	if s.Elapsed > 0 {
		b.IntoBoot = render.FormatOffset(s.Elapsed)
	}
	for _, c := range summary.OrderedCategories(s) {
		b.Subsystems[summary.CategoryLabel(c)] = s.Categories[c]
	}
	return b
}

func timingJSON(r Report) jsonTiming {
	t := r.Timing
	jt := jsonTiming{
		Total:      render.Duration(t.Total),
		Source:     timingSourceJSON(t),
		Phases:     []jsonPhase{},
		Milestones: []jsonMilestone{},
	}
	for _, p := range t.Phases {
		jt.Phases = append(jt.Phases, jsonPhase{Name: p.Name, Duration: render.Duration(p.Duration)})
	}
	for _, m := range t.Milestones {
		jt.Milestones = append(jt.Milestones, jsonMilestone{Name: m.Name, At: render.FormatOffset(m.At)})
	}
	return jt
}

func servicesJSON(r Report) jsonServices {
	rep := r.Services
	js := jsonServices{Failed: []jsonFailure{}, Slow: []jsonSlow{}}
	for _, f := range rep.Failed {
		js.Failed = append(js.Failed, jsonFailure{
			Unit:    f.Unit,
			Message: f.Message,
			At:      render.FormatOffset(f.At),
			Count:   f.Count,
		})
	}
	for _, s := range rep.Slow {
		js.Slow = append(js.Slow, jsonSlow{
			Name:     s.Name,
			Duration: render.Duration(s.Duration),
			Started:  render.FormatOffset(s.Started),
		})
	}
	return js
}
