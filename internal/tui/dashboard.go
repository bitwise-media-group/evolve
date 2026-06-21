// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/bitwise-media-group/evolve/internal/run"
)

// status is the lifecycle state of one execution unit or case.
type status int

const (
	stPending status = iota
	stRunning
	stPass
	stFail
	stError
	stSkipped
	stCount // count-only: token estimates, no pass/fail
)

// terminal reports whether a status is a settled outcome (no longer pending or
// running).
func (s status) terminal() bool {
	return s == stPass || s == stFail || s == stError || s == stSkipped || s == stCount
}

// statusOf maps an engine item status to a dashboard status.
func statusOf(s run.Status) status {
	switch s {
	case run.StatusPass:
		return stPass
	case run.StatusFail:
		return stFail
	case run.StatusError:
		return stError
	case run.StatusSkip:
		return stSkipped
	default:
		return stPending
	}
}

// caseState is one trigger query or eval within a unit, with its live outcome
// and the per-case figures the engine streams as it finishes. output is the
// agent's final text (evals only) and verdict the rendered grading block; both
// are retained so the Executing pane can show what each run produced.
type caseState struct {
	kind    run.Kind
	label   string
	status  status
	metrics run.ItemMetrics
	output  string // capped head of the agent's final text (full text is in logPath)
	verdict string
	workdir string // retained workspace dir (o opens it); empty until retained
	logPath string // full output log file (l opens it); empty for triggers
}

// unitState is one (skill, model, tier) execution unit.
type unitState struct {
	ref      run.UnitRef
	plugin   string
	display  string // provider/model label
	status   status
	mode     run.Mode
	total    int
	done     int
	passed   int
	failed   int
	errored  int
	reason   string // skip reason
	savedRel string
	cases    []*caseState
	byLabel  map[string]*caseState
}

// inflight is one case currently executing, tracked so the detail panel can show
// what is in progress (the engine runs several at once under --jobs).
type inflight struct {
	ref   run.UnitRef
	label string
	start time.Time
}

// execItem points at one case execution in the order it started. The Executing
// pane navigates this log (newest last); the case it names is resolved live so
// the row reflects the latest status/metrics/output.
type execItem struct {
	ref   run.UnitRef
	label string
}

// the four rollup slices in the Rollup panel.
type tab int

const (
	tabSummary tab = iota
	tabProviders
	tabPlugins
	tabSkills
	tabCount
)

// the focusable panes, in Tab-cycle order: the left Execution tree first, then
// the three right-column panes.
type pane int

const (
	paneExecution pane = iota
	paneRollup
	paneRuns
	paneDetails
	paneN
)

// Tree grouping: units are grouped plugin → skill → model for the left pane. The
// grouping is fixed at construction (the plan does not change mid-run); live
// status is read from the units it points at.
type (
	modelGroup struct {
		key     string
		display string
		units   []int // indices into dashboardModel.units (triggers before evals)
	}
	skillGroup struct {
		skill  string
		title  string
		models []modelGroup
	}
	pluginGroup struct {
		name   string
		skills []skillGroup
	}
)

type dashboardModel struct {
	cat      []run.SkillCatalog
	skillCat map[string]*run.SkillCatalog
	units    []*unitState
	index    map[run.UnitRef]int
	tree     []pluginGroup

	spin     spinner.Model
	warnings []string
	done     bool
	failed   bool

	tab   tab
	focus pane // which pane (Execution/Rollup/Runs/Details) has key focus
	// runSel is the shared selection: an index into execLog that the Execution,
	// Runs, and Details panes all reflect. runFollow keeps it pinned to the newest
	// execution as new ones arrive.
	runSel       int
	runFollow    bool
	detailScroll int  // scroll offset into the Details result body
	confirmQuit  bool // the quit-confirmation dialog is showing

	// Execution-pane browse state. Only live while paneExecution is focused;
	// setFocus seeds it on entry and clears it on leave, so the pane otherwise
	// reflects the shared selection.
	execBrowse bool             // Execution pane is focused → user-navigable
	execSel    int              // browse cursor: index into buildNodeRefsWith(browseExpanded)
	execExpand map[nodeKey]bool // user expand/collapse overrides (browse mode only)

	// execLog is every planned execution, pre-populated in plan order so the Runs
	// pane shows the pending ones before they start. liveIdx is the index of the
	// most recently started execution — the anchor runFollow tracks (the list is no
	// longer start-ordered, so "newest" is not simply the last row). -1 until the
	// first execution starts.
	execLog  []execItem
	inflight []inflight
	liveIdx  int

	started   bool
	startWall time.Time
	now       func() time.Time

	w, h int
}

func newDashboard(cat []run.SkillCatalog, plan []run.UnitRef, filter *run.Filter) dashboardModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot

	d := dashboardModel{
		cat:       cat,
		skillCat:  map[string]*run.SkillCatalog{},
		index:     map[run.UnitRef]int{},
		spin:      sp,
		now:       time.Now,
		focus:     paneRuns,
		runFollow: true,
		liveIdx:   -1,
	}
	for i := range cat {
		d.skillCat[cat[i].Skill] = &cat[i]
	}

	// Order units by execution order: catalog index (plugin → skill), then the
	// order models first appear in the plan, then tier (triggers before evals).
	skillOrder := map[string]int{}
	for i := range cat {
		skillOrder[cat[i].Skill] = i
	}
	modelOrder := map[string]int{}
	for _, ref := range plan {
		if _, ok := modelOrder[ref.Key]; !ok {
			modelOrder[ref.Key] = len(modelOrder)
		}
	}
	refs := append([]run.UnitRef(nil), plan...)
	sort.SliceStable(refs, func(i, j int) bool {
		if si, sj := skillOrder[refs[i].Skill], skillOrder[refs[j].Skill]; si != sj {
			return si < sj
		}
		if mi, mj := modelOrder[refs[i].Key], modelOrder[refs[j].Key]; mi != mj {
			return mi < mj
		}
		return refs[i].Kind < refs[j].Kind
	})
	for _, ref := range refs {
		sc := d.skillCat[ref.Skill]
		u := &unitState{ref: ref, status: stPending, display: ref.Key, byLabel: map[string]*caseState{}}
		if sc != nil {
			u.plugin = sc.Plugin
		}
		d.buildCases(u, sc, filter)
		u.total = len(u.cases)
		d.index[ref] = len(d.units)
		d.units = append(d.units, u)
	}
	d.buildTree()
	d.buildExecLog()
	return d
}

// buildCases pre-populates a unit's case rows from the catalog so pending cases
// render with their labels before they run. It mirrors the engine's applicability
// (per-provider skips and the selection filter) so the rows match what runs;
// live updates are matched back by label.
func (d *dashboardModel) buildCases(u *unitState, sc *run.SkillCatalog, filter *run.Filter) {
	if sc == nil {
		return
	}
	prov := providerOf(u.ref.Key)
	if u.ref.Kind == run.KindTriggers {
		for _, t := range sc.Triggers {
			if t.SkipsProvider(prov) || !selectedCase(filter, run.KindTriggers, sc.Skill, t.Query) {
				continue
			}
			cr := &caseState{kind: run.KindTriggers, label: t.Query, status: stPending}
			u.cases = append(u.cases, cr)
			u.byLabel[t.Query] = cr
		}
		return
	}
	for _, e := range sc.Evals {
		if e.SkipsProvider(prov) || !selectedCase(filter, run.KindEvals, sc.Skill, e.ID) {
			continue
		}
		cr := &caseState{kind: run.KindEvals, label: e.ID, status: stPending}
		u.cases = append(u.cases, cr)
		u.byLabel[e.ID] = cr
	}
}

// buildTree groups the (already execution-ordered) units into plugin → skill →
// model for the left pane.
func (d *dashboardModel) buildTree() {
	for i, u := range d.units {
		if len(d.tree) == 0 || d.tree[len(d.tree)-1].name != u.plugin {
			d.tree = append(d.tree, pluginGroup{name: u.plugin})
		}
		pi := len(d.tree) - 1
		sk := d.tree[pi].skills
		if len(sk) == 0 || sk[len(sk)-1].skill != u.ref.Skill {
			title := u.ref.Skill
			if sc := d.skillCat[u.ref.Skill]; sc != nil && sc.Title != "" {
				title = sc.Title
			}
			d.tree[pi].skills = append(d.tree[pi].skills, skillGroup{skill: u.ref.Skill, title: title})
		}
		si := len(d.tree[pi].skills) - 1
		md := d.tree[pi].skills[si].models
		if len(md) == 0 || md[len(md)-1].key != u.ref.Key {
			d.tree[pi].skills[si].models = append(d.tree[pi].skills[si].models,
				modelGroup{key: u.ref.Key, display: u.display})
		}
		mi := len(d.tree[pi].skills[si].models) - 1
		d.tree[pi].skills[si].models[mi].units = append(d.tree[pi].skills[si].models[mi].units, i)
	}
}

// buildExecLog pre-populates the execution log from every unit's pre-built cases,
// in plan order, so the Runs pane lists the pending executions up front instead of
// growing as each one starts. itemStarted matches back to these rows by label
// (appending only a case the catalog did not predeclare).
func (d *dashboardModel) buildExecLog() {
	for _, u := range d.units {
		for _, c := range u.cases {
			d.execLog = append(d.execLog, execItem{ref: u.ref, label: c.label})
		}
	}
}

// selectedCase reports whether a case is part of the run. A nil filter includes
// everything; otherwise membership is the merged per-skill set the dashboard was
// built with.
func selectedCase(f *run.Filter, kind run.Kind, skill, key string) bool {
	if f == nil {
		return true
	}
	if kind == run.KindTriggers {
		return f.Triggers[skill][key]
	}
	return f.Evals[skill][key]
}

func providerOf(key string) string {
	if before, _, ok := strings.Cut(key, "/"); ok {
		return before
	}
	return key
}

// ── message handling ───────────────────────────────────────────────────────

func (d *dashboardModel) apply(msg tea.Msg) {
	switch m := msg.(type) {
	case unitStartedMsg:
		d.markStarted()
		if u := d.unit(m.ref); u != nil {
			u.status = stRunning
			if m.total > 0 {
				u.total = m.total
			}
			u.mode = m.mode
		}
	case unitSkippedMsg:
		if u := d.unit(m.ref); u != nil {
			u.status = stSkipped
			u.reason = m.reason
			// Settle the pre-populated case rows so a skipped unit's executions show
			// as skipped in the Runs pane and tree rather than perpetually pending.
			u.settlePending(stSkipped)
		}
	case itemStartedMsg:
		d.markStarted()
		if u := d.unit(m.ref); u != nil {
			cr := u.byLabel[m.item.Label]
			if cr == nil {
				cr = &caseState{kind: m.ref.Kind, label: m.item.Label}
				u.cases = append(u.cases, cr)
				u.byLabel[m.item.Label] = cr
			}
			cr.status = stRunning
			u.status = stRunning
			d.inflight = append(d.inflight, inflight{ref: m.ref, label: m.item.Label, start: d.now()})
			// The execution is normally already in the pre-populated log; append only
			// a case the catalog did not predeclare. Either way it is now the live one.
			idx := d.execLogIndex(m.ref, m.item.Label)
			if idx < 0 {
				d.execLog = append(d.execLog, execItem{ref: m.ref, label: m.item.Label})
				idx = len(d.execLog) - 1
			}
			d.liveIdx = idx
			d.followAdvance()
		}
	case itemDoneMsg:
		if u := d.unit(m.ref); u != nil {
			u.done++
			switch m.item.Status {
			case run.StatusPass:
				u.passed++
			case run.StatusError:
				u.errored++
			case run.StatusFail:
				u.failed++
			}
			if cr := u.byLabel[m.item.Label]; cr != nil {
				cr.status = statusOf(m.item.Status)
				cr.metrics = m.item.Metrics
				cr.output = m.item.Output
				cr.verdict = m.item.Detail
				cr.workdir = m.item.WorkspacePath
				cr.logPath = m.item.LogPath
			}
			d.removeInflight(m.ref, m.item.Label)
		}
	case unitFinishedMsg:
		if u := d.unit(m.ref); u != nil {
			u.savedRel = m.savedRel
			u.passed = m.sum.Passed
			u.failed = m.sum.Failed
			u.errored = m.sum.Errored
			u.total = m.sum.Total
			switch {
			case !m.sum.Executed:
				u.status = stCount
				u.settlePending(stCount)
			case u.errored > 0:
				u.status = stError
			case u.failed > 0:
				u.status = stFail
			default:
				u.status = stPass
			}
		}
	case warnMsg:
		d.warnings = append(d.warnings, strings.TrimRight(m.text, "\n"))
		if len(d.warnings) > 50 {
			d.warnings = d.warnings[len(d.warnings)-50:]
		}
	case runDoneMsg:
		d.done = true
		d.failed = m.failed
	}
}

// settlePending moves a unit's still-pending cases to s — used when a count-only
// unit finishes without per-case run results.
func (u *unitState) settlePending(s status) {
	for _, c := range u.cases {
		if c.status == stPending {
			c.status = s
		}
	}
}

func (d *dashboardModel) markStarted() {
	if !d.started {
		d.started = true
		d.startWall = d.now()
	}
}

func (d *dashboardModel) unit(ref run.UnitRef) *unitState {
	if i, ok := d.index[ref]; ok {
		return d.units[i]
	}
	return nil
}

func (d *dashboardModel) removeInflight(ref run.UnitRef, label string) {
	for i := range d.inflight {
		if d.inflight[i].ref == ref && d.inflight[i].label == label {
			d.inflight = append(d.inflight[:i], d.inflight[i+1:]...)
			return
		}
	}
}

// ── key handling ───────────────────────────────────────────────────────────

// handleKey processes a key on the dashboard; returns true if it requests quit.
// Global keys (1-4, Tab, f, o, l) switch focus, follow, and open paths from any
// pane; the rest route to whichever pane is active. The Execution pane has two
// modes: it reflects the shared selection while unfocused and becomes a navigable
// tree while focused (see enterBrowse/exitBrowse).
func (d *dashboardModel) handleKey(msg tea.KeyMsg) bool {
	key := msg.String()

	// The quit-confirmation dialog captures input until dismissed; a second
	// ctrl+c (or y/Enter) always quits.
	if d.confirmQuit {
		switch key {
		case "y", "Y", "enter", "ctrl+c":
			return true
		case "n", "N", "esc":
			d.confirmQuit = false
		}
		return false
	}

	switch key {
	case "q", "esc", "ctrl+c":
		d.confirmQuit = true
	case "1":
		d.setFocus(paneExecution)
	case "2":
		d.setFocus(paneRollup)
	case "3":
		d.setFocus(paneRuns)
	case "4":
		d.setFocus(paneDetails)
	case "tab":
		d.setFocus((d.focus + 1) % paneN)
	case "shift+tab":
		d.setFocus((d.focus + paneN - 1) % paneN)
	case "f", "F":
		d.follow()
	case "o", "O":
		openPath(d.selectedField(func(c *caseState) string { return c.workdir }))
	case "l", "L":
		openPath(d.selectedField(func(c *caseState) string { return c.logPath }))
	default:
		d.paneKey(key)
	}
	return false
}

// paneKey routes a key to the active pane: Rollup switches tabs, Runs moves the
// selection, Details scrolls the result.
func (d *dashboardModel) paneKey(key string) {
	switch d.focus {
	case paneExecution:
		d.execKey(key)
	case paneRollup:
		switch key {
		case "left", "h":
			d.tab = (d.tab + tabCount - 1) % tabCount
		case "right", "l":
			d.tab = (d.tab + 1) % tabCount
		}
	case paneRuns:
		switch key {
		case "up", "k":
			d.moveRun(-1)
		case "down", "j":
			d.moveRun(1)
		case "g", "home":
			d.runTop()
		case "G", "end":
			d.runBottom()
		case "ctrl+d", "pgdown":
			d.moveRun(d.runPageStep())
		case "ctrl+u", "pgup":
			d.moveRun(-d.runPageStep())
		case "enter", " ":
			d.detailScroll = 0
			d.setFocus(paneDetails)
		}
	case paneDetails:
		switch key {
		case "up", "k":
			d.scrollDetailBy(-1)
		case "down", "j":
			d.scrollDetailBy(1)
		case "g", "home":
			d.detailScroll = 0
		case "G", "end":
			d.detailScroll = d.maxDetailScroll()
		case "ctrl+d", "pgdown":
			d.scrollDetailBy(d.detailPageStep())
		case "ctrl+u", "pgup":
			d.scrollDetailBy(-d.detailPageStep())
		}
	}
}

// setFocus changes the active pane. Leaving Details resumes follow (it is paused
// while Details is active so the result under review stays selected). Entering the
// Execution pane starts browse mode; leaving it keeps the shared selection put.
func (d *dashboardModel) setFocus(p pane) {
	if d.focus == paneDetails && p != paneDetails {
		d.resumeFollow()
	}
	if d.focus == paneExecution && p != paneExecution {
		d.exitBrowse()
	}
	if p == paneExecution && d.focus != paneExecution {
		d.enterBrowse()
	}
	d.focus = p
}

// enterBrowse switches the Execution pane into user-navigable browse mode. It
// seeds the expansion set from the current view, then lands the cursor on the
// shared selection's case (expanding its path so it is visible).
func (d *dashboardModel) enterBrowse() {
	d.execBrowse = true
	d.execExpand = map[nodeKey]bool{}
	for _, n := range d.buildNodeRefs() {
		switch n.kind {
		case nkPlugin, nkSkill, nkModel:
			if !n.collapsed {
				d.execExpand[keyOf(n)] = true
			}
		}
	}
	d.execSel = 0
	d.syncExecToSel()
}

// exitBrowse leaves browse mode: it discards the browse cursor and expansion so
// the pane reverts to reflecting the shared selection. It does not re-follow — the
// selection the user navigated to stays put (jumping back to the live case with
// Details left behind is jarring).
func (d *dashboardModel) exitBrowse() {
	d.execBrowse = false
	d.execSel = 0
	d.execExpand = nil
}

// ── Runs pane: the execution log ────────────────────────────────────────────

// follow jumps the shared selection to the live (most recently started) execution
// and tracks it from now on. It is global (the [f] key), so it works whichever
// pane is focused; in browse mode it also moves the tree cursor.
func (d *dashboardModel) follow() {
	d.runFollow = true
	if d.liveIdx >= 0 {
		d.runSel = d.liveIdx
	}
	d.detailScroll = 0
	if d.execBrowse {
		d.syncExecToSel()
	}
}

// followAdvance moves the shared selection onto a freshly-started execution while
// following — unless Details is active, which pauses following so the result
// under review stays selected.
func (d *dashboardModel) followAdvance() {
	if d.focus != paneDetails {
		d.resumeFollow()
	}
}

// resumeFollow snaps the shared selection back to the live execution if it was
// following, and moves the browse cursor with it when the tree is focused.
func (d *dashboardModel) resumeFollow() {
	if !d.runFollow {
		return
	}
	if d.liveIdx >= 0 && d.runSel != d.liveIdx {
		d.runSel = d.liveIdx
		d.detailScroll = 0
	}
	if d.execBrowse {
		d.syncExecToSel()
	}
}

// moveRun moves the Runs selection by delta, following only while it rests on the
// live execution. A changed selection resets the Details scroll so the new
// execution starts at the top.
func (d *dashboardModel) moveRun(delta int) {
	n := len(d.execLog)
	if n == 0 || delta == 0 {
		return
	}
	prev := d.runSel
	d.runSel = clampInt(d.runSel+delta, 0, n-1)
	d.runFollow = d.runSel == d.liveIdx
	if d.runSel != prev {
		d.detailScroll = 0
	}
}

func (d *dashboardModel) runTop() {
	if d.runSel != 0 {
		d.detailScroll = 0
	}
	d.runSel = 0
	d.runFollow = d.liveIdx == 0
}

// runBottom jumps to the last execution in the list (the [G] key). The list is
// plan-ordered, so the bottom is the last planned execution, not necessarily the
// live one — following re-engages only if they coincide.
func (d *dashboardModel) runBottom() {
	n := len(d.execLog)
	if n == 0 {
		return
	}
	if d.runSel != n-1 {
		d.detailScroll = 0
	}
	d.runSel = n - 1
	d.runFollow = d.runSel == d.liveIdx
}

// runPageStep is the Runs list's visible height, so ctrl+d/ctrl+u page by a
// screenful of executions.
func (d dashboardModel) runPageStep() int {
	_, _, runsH, _ := d.rightDims()
	return max(runsH-2, 1)
}

// currentRun is the execution Runs has selected and Details mirrors, or -1 when
// nothing has started.
func (d dashboardModel) currentRun() int {
	n := len(d.execLog)
	if n == 0 {
		return -1
	}
	return clampInt(d.runSel, 0, n-1)
}

// selectedField returns a field of the currently-selected execution's case, or
// "" when nothing is selected or the case has no such path yet.
func (d dashboardModel) selectedField(get func(*caseState) string) string {
	sel := d.currentRun()
	if sel < 0 {
		return ""
	}
	if c := d.caseFor(d.execLog[sel]); c != nil {
		return get(c)
	}
	return ""
}

// ── Details pane: the scrollable result ─────────────────────────────────────

func (d *dashboardModel) scrollDetailBy(delta int) {
	d.detailScroll = clampInt(d.detailScroll+delta, 0, d.maxDetailScroll())
}

// maxDetailScroll is how far the Details result can scroll given the current
// selection and pane height.
func (d dashboardModel) maxDetailScroll() int {
	sel := d.currentRun()
	if sel < 0 {
		return 0
	}
	w, _, _, detailsH := d.rightDims()
	item := d.execLog[sel]
	resultH := max(detailsH-2-len(d.detailHeader(item, w)), 1)
	return max(0, len(d.detailResult(item, w))-resultH)
}

func (d dashboardModel) detailPageStep() int {
	_, _, _, detailsH := d.rightDims()
	return max((detailsH-2)/2, 1)
}

// openPath launches the OS file handler on path (a retained workspace dir or an
// output log) as a detached, best-effort side effect. A blank path is a no-op,
// so it is safe to call before the engine has surfaced these paths.
func openPath(path string) {
	if path == "" {
		return
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{path}
	case "windows":
		name, args = "cmd", []string{"/c", "start", "", path}
	default:
		name, args = "xdg-open", []string{path}
	}
	_ = exec.Command(name, args...).Start()
}
