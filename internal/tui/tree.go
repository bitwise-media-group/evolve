// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import (
	"slices"

	"github.com/bitwise-media-group/evolve/internal/plan"
)

// treeNode is one row in the selection form's plugin → skill → case tree. The
// tree is pure structure and navigation: selection state lives in the
// plan.Session, and rows render their glyph by querying it, so the tree never
// holds an authoritative on/off. Case leaves carry the (skill, kind, case)
// identity that forms a plan.CaseRef; parents carry only their label and the
// skill/kind they scope.
type treeNode struct {
	label    string
	depth    int
	parent   int   // index into tree.nodes, -1 at the top level
	children []int // indices into tree.nodes
	leaf     bool
	expanded bool

	skill   string    // skill / case nodes -> the skill the node scopes
	kind    plan.Kind // case nodes -> which tier
	caseKey string    // case leaf -> trigger query or eval id
	hasKind bool      // skill-tier nodes and case leaves scope a single tier
}

// tree is a navigable, collapsible structure tree.
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

// caseLeaves returns the CaseRefs of every case leaf at or under node i — the
// set a plugin/skill/case toggle applies to.
func (t *tree) caseLeaves(i int) []plan.CaseRef {
	if t.nodes[i].leaf {
		return []plan.CaseRef{{Skill: t.nodes[i].skill, Kind: t.nodes[i].kind, Case: t.nodes[i].caseKey}}
	}
	var out []plan.CaseRef
	for _, c := range t.nodes[i].children {
		out = append(out, t.caseLeaves(c)...)
	}
	return out
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

// expand collapses or expands the node under the cursor; on a leaf, right is a
// no-op and left jumps to the parent.
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

// expandWhere expands every parent that has a leaf matching pred and collapses
// the rest, so the initial view reveals the preselected cases while staying
// compact.
func (t *tree) expandWhere(pred func(plan.CaseRef) bool) {
	for i := range t.nodes {
		if t.nodes[i].leaf || len(t.nodes[i].children) == 0 {
			continue
		}
		open := slices.ContainsFunc(t.caseLeaves(i), pred)
		t.nodes[i].expanded = open
	}
	t.cursor = 0
}

// expandLevel expands every collapsed node at the shallowest currently-foldable
// depth — one level of the whole tree opens at a time.
func (t *tree) expandLevel() {
	best := -1
	for i := range t.nodes {
		if t.foldable(i) && !t.nodes[i].expanded && t.nodeVisible(i) {
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
