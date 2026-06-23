// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/bitwise-media-group/evolve/internal/plan"
)

func TestFormRendersAndPreselects(t *testing.T) {
	m := testModel(t)
	m = step(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.View().Content
	for _, want := range []string{"Filters", "Harnesses", "Models", "Plugins"} {
		if !strings.Contains(out, want) {
			t.Errorf("form view missing pane %q:\n%s", want, out)
		}
	}
	// The glyph legend sits under the tree (the terminal is tall enough to show it).
	for _, want := range []string{"Legend", "trigger", "eval", "forced on", "auto (all run)"} {
		if !strings.Contains(out, want) {
			t.Errorf("form view missing legend entry %q:\n%s", want, out)
		}
	}
	if !m.form.valid() {
		t.Error("form should be valid: m1 enabled and every case auto-queued")
	}
	req := m.form.request()
	if len(req.Models) != 1 || req.Models[0].Model.ID != "fake/m1" {
		t.Fatalf("models = %+v, want only fake/m1", req.Models)
	}
}

// TestFormFilterToggle: toggling the failed filter routes to the session.
func TestFormFilterToggle(t *testing.T) {
	m := testModel(t)
	m = step(m, runeKey("1")) // focus the filters pane
	m = step(m, runeKey("j")) // new -> modified
	m = step(m, runeKey("j")) // modified -> failed
	m = step(m, runeKey(" ")) // toggle failed on
	if !m.form.session.FilterState().Failed {
		t.Error("failed filter should be on after toggling its row")
	}
	if m.form.session.FilterState().New || m.form.session.FilterState().Modified {
		t.Error("only the failed filter should be on")
	}
}

// TestFormNodeCycle: cycling a case routes to the session and unqueues it (the
// first press on an auto-queued case turns it off).
func TestFormNodeCycle(t *testing.T) {
	m := testModel(t)
	m = step(m, runeKey("4")) // focus the tree (fully expanded: every case queued)
	// Rows: 0 plugin, 1 skill, 2 q1, 3 q2, 4 e1, 5 e2. Move to q1.
	m = step(m, runeKey("j"))
	m = step(m, runeKey("j"))
	cr := plan.CaseRef{Skill: "solo-skill", Kind: plan.KindTriggers, Case: "q1"}
	if got := m.form.session.NodeSel([]plan.CaseRef{cr}); got != plan.SelAutoAll {
		t.Fatalf("q1 starts %v, want SelAutoAll", got)
	}
	m = step(m, runeKey(" ")) // auto -> off
	if got := m.form.session.NodeSel([]plan.CaseRef{cr}); got != plan.SelForceOff {
		t.Errorf("after toggle q1 = %v, want SelForceOff", got)
	}
	// The resolved plan must no longer queue q1 for m1.
	for _, pl := range m.form.session.Plan().Plugins {
		for _, sk := range pl.Skills {
			for _, mdl := range sk.Models {
				for _, u := range mdl.Units {
					for _, c := range u.Cases {
						if c.Label == "q1" && c.Queued {
							t.Error("q1 should not be queued after forcing it off")
						}
					}
				}
			}
		}
	}
}

// TestFormProviderToggle: a provider header row toggles every model under it.
func TestFormProviderToggle(t *testing.T) {
	m := testModel(t)
	m = step(m, runeKey("3")) // focus Models; the cursor starts on the Fake header
	if it, ok := m.form.models.current(); !ok || !it.header {
		t.Fatalf("models cursor should start on a provider header, got %+v", it)
	}
	// Only fake/m1 starts enabled; toggling the header enables the whole provider.
	m = step(m, runeKey(" "))
	if !m.form.session.ModelEnabled("fake/m1") || !m.form.session.ModelEnabled("fake/m2") {
		t.Error("toggling the provider header should enable all its models")
	}
	// Toggling again, now that all are on, disables the whole provider.
	m = step(m, runeKey(" "))
	if m.form.session.ModelEnabled("fake/m1") || m.form.session.ModelEnabled("fake/m2") {
		t.Error("toggling a fully-enabled provider header should disable all its models")
	}
}

// TestFormButtonNav: the button row is tab-reachable and left/right + enter pick
// and activate CANCEL or RUN.
func TestFormButtonNav(t *testing.T) {
	f := testModel(t).form
	// filters -> harness -> models -> tree -> buttons.
	for range 4 {
		f, _ = f.update("tab")
	}
	if f.focus != paneButtons {
		t.Fatalf("after 4 tabs focus = %d, want paneButtons(%d)", f.focus, paneButtons)
	}
	if f.btnFocus != btnRun {
		t.Fatalf("entering the button row should focus RUN, got %d", f.btnFocus)
	}
	// m1 is enabled, so the form is valid and enter on RUN runs.
	if _, act := f.update("enter"); act != actionRun {
		t.Errorf("enter on RUN = %v, want actionRun", act)
	}
	f, _ = f.update("left")
	if f.btnFocus != btnCancel {
		t.Fatalf("left should focus CANCEL, got %d", f.btnFocus)
	}
	if _, act := f.update("enter"); act != actionCancel {
		t.Errorf("enter on CANCEL = %v, want actionCancel", act)
	}
}

// TestFormLegendResponsive: the legend is one row when it fits the pane and two
// when it does not, so the tree reclaims a row on a wide terminal.
func TestFormLegendResponsive(t *testing.T) {
	f := testModel(t).form
	if body, h := f.legend(200); h != 3 || strings.Contains(body, "\n") {
		t.Errorf("wide legend = (h=%d, %q), want a single-row height 3", h, body)
	}
	if body, h := f.legend(40); h != 4 || !strings.Contains(body, "\n") {
		t.Errorf("narrow legend = (h=%d, %q), want a two-row height 4", h, body)
	}
}

// TestFormRequestMatchesPlan: the RunRequest re-Builds to the same plan the form
// previews, so the engine and dashboard cannot drift from the form.
func TestFormRequestMatchesPlan(t *testing.T) {
	m := testModel(t)
	req := m.form.request()
	rebuilt := plan.Build(m.cat, req.Models, req.Selection, plan.PriorMetrics{})
	preview := m.form.session.Plan()
	if len(rebuilt.Plugins) != len(preview.Plugins) {
		t.Fatalf("rebuilt %d plugins, preview %d", len(rebuilt.Plugins), len(preview.Plugins))
	}
	countQueued := func(p plan.Plan) int {
		n := 0
		for _, pl := range p.Plugins {
			for _, sk := range pl.Skills {
				for _, mdl := range sk.Models {
					for _, u := range mdl.Units {
						for _, c := range u.Cases {
							if c.Queued {
								n++
							}
						}
					}
				}
			}
		}
		return n
	}
	if countQueued(rebuilt) != countQueued(preview) {
		t.Errorf("rebuilt queued %d != preview queued %d", countQueued(rebuilt), countQueued(preview))
	}
}
