// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// TestFormLayoutMatchesView aligns the layout rects against the rendered
// frame: each pane's title sits on the row its rect starts at and the buttons
// render inside their rects — the drift guard between layout() and view().
func TestFormLayoutMatchesView(t *testing.T) {
	f := testModel(t).form
	f.w, f.h = 120, 40

	l := f.layout()
	lines := strings.Split(f.view(), "\n")
	for _, tc := range []struct {
		row  int
		want string
	}{
		{row: l.filters.y0, want: "Filters"},
		{row: l.harness.y0, want: "Harnesses"},
		{row: l.models.y0, want: "Models"},
		{row: l.tree.y0, want: "Plugins / Skills / Cases"},
	} {
		if tc.row >= len(lines) || !strings.Contains(lines[tc.row], tc.want) {
			t.Errorf("row %d must hold %q:\n%s", tc.row, tc.want, f.view())
		}
	}

	// The button labels sit on the middle border row, inside their rects.
	mid := ansi.Strip(lines[l.runBtn.y0+1])
	for label, r := range map[string]rect{btnCancelLabel: l.cancelBtn, btnRunLabel: l.runBtn} {
		idx := strings.Index(mid, label)
		if idx < 0 {
			t.Fatalf("button row %q missing %q", mid, label)
		}
		if col := ansi.StringWidth(mid[:idx]); !r.contains(col, l.runBtn.y0+1) {
			t.Errorf("label %q at column %d, outside its rect %+v", label, col, r)
		}
	}
}

// TestFormMouseListClicks pins click-to-toggle on the checkbox lists: the
// click focuses the pane, moves the cursor onto the row, and routes the same
// Session toggle the space key uses; empty space below a list only focuses.
func TestFormMouseListClicks(t *testing.T) {
	f := testModel(t).form
	f.w, f.h = 120, 40
	l := f.layout()

	c := contentRect(l.filters)
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+1)) // the "modified" row
	if f.focus != paneFilters || f.filters.cursor != 1 || !f.session.FilterState().Modified {
		t.Errorf("focus=%d cursor=%d modified=%v, want the clicked filter focused and on",
			f.focus, f.filters.cursor, f.session.FilterState().Modified)
	}

	c = contentRect(l.models)
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+1)) // m1 under the provider header
	if f.focus != paneModels || f.session.ModelEnabled("fake/m1") {
		t.Errorf("focus=%d m1=%v, want the clicked model toggled off", f.focus, f.session.ModelEnabled("fake/m1"))
	}
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0)) // the provider header row
	if !f.session.ModelEnabled("fake/m1") || !f.session.ModelEnabled("fake/m2") {
		t.Error("clicking the provider header must enable the whole provider")
	}

	f.focus = paneFilters
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+10)) // below the last model row
	if f.focus != paneModels || f.session.ModelEnabled("fake/m1") != true {
		t.Error("a click on empty list space must focus the pane and change nothing else")
	}
}

// TestFormMouseTree pins the tree click semantics: parents fold and unfold,
// the first click on a leaf only takes the cursor, and a repeat click cycles
// the leaf's selection through the Session.
func TestFormMouseTree(t *testing.T) {
	f := testModel(t).form
	f.w, f.h = 120, 40
	l := f.layout()
	c := contentRect(l.tree)

	// Rows: 0 plugin, 1 skill, 2 q1, 3 q2, 4 e1, 5 e2.
	cr := plan.CaseRef{Skill: "solo-skill", Kind: plan.KindTriggers, Case: "q1"}
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+2)) // first click: cursor only
	if f.focus != paneTree || f.tree.cursor != 2 {
		t.Fatalf("focus=%d cursor=%d, want the tree focused with the cursor on q1", f.focus, f.tree.cursor)
	}
	if got := f.session.NodeSel([]plan.CaseRef{cr}); got != plan.SelAutoAll {
		t.Fatalf("first click changed q1 to %v, want SelAutoAll untouched", got)
	}
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+2)) // repeat click: cycle
	if got := f.session.NodeSel([]plan.CaseRef{cr}); got != plan.SelForceOff {
		t.Errorf("repeat click q1 = %v, want SelForceOff", got)
	}

	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+1)) // the skill parent: fold
	if n := len(f.tree.visible()); n != 2 || f.tree.cursor != 1 {
		t.Errorf("visible=%d cursor=%d, want the skill folded to 2 rows under the cursor", n, f.tree.cursor)
	}
	f, _ = f.handleMouse(leftClick(c.x0+2, c.y0+1)) // and unfold
	if n := len(f.tree.visible()); n != 6 {
		t.Errorf("visible=%d, want the skill unfolded back to 6 rows", n)
	}
}

// TestFormMouseButtons pins the button clicks: CANCEL cancels, RUN runs only
// while a run is queued, and a disabled RUN click still lands focus there.
func TestFormMouseButtons(t *testing.T) {
	f := testModel(t).form
	f.w, f.h = 120, 40
	l := f.layout()

	if _, act := f.handleMouse(leftClick(l.runBtn.x0+2, l.runBtn.y0+1)); act != actionRun {
		t.Errorf("RUN click = %v, want actionRun", act)
	}
	if _, act := f.handleMouse(leftClick(l.cancelBtn.x0+2, l.cancelBtn.y0+1)); act != actionCancel {
		t.Errorf("CANCEL click = %v, want actionCancel", act)
	}

	f.session.EnableModel("fake/m1", false) // nothing queued: RUN goes inert
	f2, act := f.handleMouse(leftClick(l.runBtn.x0+2, l.runBtn.y0+1))
	if act != actionNone || f2.focus != paneButtons || f2.btnFocus != btnRun {
		t.Errorf("inert RUN click = (%v, focus %d/%d), want no action with RUN focused", act, f2.focus, f2.btnFocus)
	}
}

// TestFormMouseWheel pins wheel-under-cursor on the form: the pane under the
// mouse moves its cursor (its windows are cursor-anchored) and focus stays.
func TestFormMouseWheel(t *testing.T) {
	f := testModel(t).form
	f.w, f.h = 120, 40
	l := f.layout()

	wheel := func(x, y int, b tea.MouseButton) {
		f, _ = f.handleMouse(tea.MouseWheelMsg{X: x, Y: y, Button: b})
	}
	wheel(l.tree.x0+3, l.tree.y0+2, tea.MouseWheelDown)
	if f.focus != paneFilters || f.tree.cursor != 1 {
		t.Errorf("focus=%d cursor=%d, want the tree scrolled without taking focus", f.focus, f.tree.cursor)
	}
	wheel(l.tree.x0+3, l.tree.y0+2, tea.MouseWheelUp)
	wheel(l.tree.x0+3, l.tree.y0+2, tea.MouseWheelUp) // clamps at the top
	if f.tree.cursor != 0 {
		t.Errorf("cursor=%d, want the wheel clamped at the top", f.tree.cursor)
	}
	wheel(l.models.x0+3, l.models.y0+1, tea.MouseWheelDown)
	if f.models.cursor != 1 || f.focus != paneFilters {
		t.Errorf("models cursor=%d focus=%d, want the models list scrolled without focus", f.models.cursor, f.focus)
	}
}
