// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import "github.com/bitwise-media-group/evolve/internal/run"

// nodeState is a leaf's tri-state selection. Partial is an initial-only state
// (from --new analysis): once the user toggles a leaf it becomes on or off and
// can never return to partial.
type nodeState int

const (
	nodeOff nodeState = iota
	nodePartial
	nodeOn
)

// treeNode is one row in a checkbox tree. Parents derive their state from their
// descendant leaves; only leaves carry an authoritative state.
type treeNode struct {
	label    string
	note     string // grey annotation shown beside a partial leaf
	depth    int
	parent   int   // index into tree.nodes, -1 at the top level
	children []int // indices into tree.nodes
	leaf     bool
	expanded bool
	state    nodeState // authoritative for leaves only

	// payloads, by node role:
	selIdx  int             // model leaf -> index into the selections slice
	skill   string          // skill / tier / case nodes
	kind    run.Kind        // tier / case nodes -> which tier
	caseKey string          // case leaf -> trigger query or eval id
	skip    map[string]bool // case leaf -> provider names this case skips
}

// tree is a navigable, collapsible checkbox tree.
type tree struct {
	nodes  []treeNode
	cursor int // position within the currently visible rows
}

// add appends a node and returns its index, registering it with its parent.
func (t *tree) add(n treeNode) int {
	idx := len(t.nodes)
	t.nodes = append(t.nodes, n)
	if n.parent >= 0 {
		t.nodes[n.parent].children = append(t.nodes[n.parent].children, idx)
	}
	return idx
}

// visible returns the indices of nodes whose ancestors are all expanded.
func (t *tree) visible() []int {
	var out []int
	for i := range t.nodes {
		if t.nodeVisible(i) {
			out = append(out, i)
		}
	}
	return out
}

func (t *tree) nodeVisible(i int) bool {
	// Walk upward instead of tracking visibility on each node; a row is visible
	// exactly when every ancestor on its path to the root is expanded.
	for p := t.nodes[i].parent; p >= 0; p = t.nodes[p].parent {
		if !t.nodes[p].expanded {
			return false
		}
	}
	return true
}

// leaves returns every leaf descendant of node i (or i itself if it is a leaf).
func (t *tree) leaves(i int) []int {
	if t.nodes[i].leaf {
		return []int{i}
	}
	var out []int
	for _, c := range t.nodes[i].children {
		// Parent nodes have no authoritative state, so recurse until we reach the
		// leaves that actually carry on/off/partial.
		out = append(out, t.leaves(c)...)
	}
	return out
}

// checkState reports the aggregate state under i: nodeOff (no leaf selected),
// nodeOn (every leaf fully on), or nodePartial (anything in between, including
// any partial leaf).
func (t *tree) checkState(i int) nodeState {
	leaves := t.leaves(i)
	if len(leaves) == 0 {
		return nodeOff
	}
	on, off := 0, 0
	for _, l := range leaves {
		// Partial leaves deliberately increment neither counter. That makes any
		// mixed branch, or any branch containing a partial leaf, aggregate to partial.
		switch t.nodes[l].state {
		case nodeOn:
			on++
		case nodeOff:
			off++
		}
	}
	switch {
	case on == len(leaves):
		return nodeOn
	case off == len(leaves):
		return nodeOff
	default:
		return nodePartial
	}
}

// toggle flips a leaf (a partial leaf becomes fully on), or sets every leaf
// under a parent to on unless the whole branch is already on, in which case it
// turns the branch off. Either way a touched leaf loses its partial state.
func (t *tree) toggle(i int) {
	if t.nodes[i].leaf {
		if t.nodes[i].state == nodeOn {
			t.nodes[i].state = nodeOff
		} else {
			t.nodes[i].state = nodeOn
		}
		t.nodes[i].note = "" // a manual change supersedes the preselection reason
		return
	}
	target := nodeOn
	if t.checkState(i) == nodeOn {
		target = nodeOff
	}
	for _, l := range t.leaves(i) {
		// Writing only leaves keeps parent state derived and clears partial markers
		// once the user explicitly touches this branch.
		t.nodes[l].state = target
		t.nodes[l].note = ""
	}
}

// currentNode returns the node index under the cursor, or -1 when empty.
func (t *tree) currentNode() int {
	vis := t.visible()
	if len(vis) == 0 {
		return -1
	}
	if t.cursor >= len(vis) {
		t.cursor = len(vis) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	return vis[t.cursor]
}

func (t *tree) move(delta int) {
	n := len(t.visible())
	if n == 0 {
		return
	}
	t.cursor += delta
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= n {
		t.cursor = n - 1
	}
}

func (t *tree) top()    { t.cursor = 0 }
func (t *tree) bottom() { t.cursor = len(t.visible()) - 1 }

// setExpand collapses or expands the node under the cursor; on a leaf, l/right
// is a no-op and h/left jumps to the parent.
func (t *tree) expand(open bool) {
	i := t.currentNode()
	if i < 0 {
		return
	}
	if t.nodes[i].leaf || len(t.nodes[i].children) == 0 {
		if !open && t.nodes[i].parent >= 0 {
			t.selectNode(t.nodes[i].parent)
		}
		return
	}
	if t.nodes[i].expanded == open {
		if !open && t.nodes[i].parent >= 0 {
			t.selectNode(t.nodes[i].parent)
		}
		return
	}
	t.nodes[i].expanded = open
}

// selectNode moves the cursor onto a specific node index (if visible).
func (t *tree) selectNode(idx int) {
	for pos, v := range t.visible() {
		if v == idx {
			t.cursor = pos
			return
		}
	}
}

// anyChecked reports whether any leaf will run (is on or partial).
func (t *tree) anyChecked() bool {
	for i := range t.nodes {
		if t.nodes[i].leaf && t.nodes[i].state != nodeOff {
			return true
		}
	}
	return false
}

// counts returns the number of leaves that will run (on or partial) and the
// total leaf count.
func (t *tree) counts() (selected, total int) {
	for i := range t.nodes {
		if t.nodes[i].leaf {
			total++
			if t.nodes[i].state != nodeOff {
				selected++
			}
		}
	}
	return selected, total
}

// collapseUnselected expands every parent that contains a selected leaf and
// collapses the rest, so the initial view is as compact as possible while still
// revealing the preselected cases.
func (t *tree) collapseUnselected() {
	for i := range t.nodes {
		if t.nodes[i].leaf || len(t.nodes[i].children) == 0 {
			continue
		}
		t.nodes[i].expanded = t.checkState(i) != nodeOff
	}
	t.cursor = 0
}

// expandLevel expands every collapsed node at the shallowest currently-foldable
// depth — one level of the whole tree opens at a time.
func (t *tree) expandLevel() {
	best := -1
	for i := range t.nodes {
		if t.foldable(i) && !t.nodes[i].expanded && t.nodeVisible(i) {
			// Find the shallowest collapsed row that is currently reachable; deeper
			// rows hidden behind it should wait for a later expand-level command.
			if best == -1 || t.nodes[i].depth < best {
				best = t.nodes[i].depth
			}
		}
	}
	if best == -1 {
		return
	}
	for i := range t.nodes {
		if t.foldable(i) && !t.nodes[i].expanded && t.nodes[i].depth == best && t.nodeVisible(i) {
			// Open every peer at that depth so repeated calls reveal the tree one
			// whole level at a time.
			t.nodes[i].expanded = true
		}
	}
}

// collapseLevel collapses every expanded node at the deepest currently-open
// depth — one level of the whole tree folds at a time.
func (t *tree) collapseLevel() {
	best := -1
	for i := range t.nodes {
		if t.foldable(i) && t.nodes[i].expanded && t.nodeVisible(i) && t.nodes[i].depth > best {
			// Collapse from the bottom upward so parents stay visible while their
			// deepest open children are folded.
			best = t.nodes[i].depth
		}
	}
	if best == -1 {
		return
	}
	for i := range t.nodes {
		if t.foldable(i) && t.nodes[i].expanded && t.nodes[i].depth == best && t.nodeVisible(i) {
			t.nodes[i].expanded = false
		}
	}
	t.move(0) // clamp the cursor back into the now-shorter visible set
}

func (t *tree) foldable(i int) bool {
	return !t.nodes[i].leaf && len(t.nodes[i].children) > 0
}
