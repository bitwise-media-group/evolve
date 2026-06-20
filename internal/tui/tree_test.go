// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

import "testing"

func TestTreeCheckState(t *testing.T) {
	cases := []struct {
		name   string
		states []nodeState
		want   nodeState
	}{
		{"no leaves", nil, nodeOff},
		{"all off", []nodeState{nodeOff, nodeOff}, nodeOff},
		{"all on", []nodeState{nodeOn, nodeOn}, nodeOn},
		{"mixed on and off", []nodeState{nodeOn, nodeOff}, nodePartial},
		{"single partial", []nodeState{nodePartial}, nodePartial},
		{"partial with on", []nodeState{nodePartial, nodeOn}, nodePartial},
		{"partial with off", []nodeState{nodePartial, nodeOff}, nodePartial},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := treeWithLeafStates(c.states...)
			if got := tr.checkState(0); got != c.want {
				t.Errorf("checkState(root) = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTreeToggle(t *testing.T) {
	cases := []struct {
		name       string
		toggleRoot bool
		states     []nodeState
		want       []nodeState
	}{
		{"off leaf turns on", false, []nodeState{nodeOff}, []nodeState{nodeOn}},
		{"on leaf turns off", false, []nodeState{nodeOn}, []nodeState{nodeOff}},
		{"partial leaf turns on", false, []nodeState{nodePartial}, []nodeState{nodeOn}},
		{"off branch turns all on", true, []nodeState{nodeOff, nodeOff}, []nodeState{nodeOn, nodeOn}},
		{"on branch turns all off", true, []nodeState{nodeOn, nodeOn}, []nodeState{nodeOff, nodeOff}},
		{"mixed branch turns all on", true, []nodeState{nodeOn, nodeOff}, []nodeState{nodeOn, nodeOn}},
		{"partial branch turns all on", true, []nodeState{nodePartial, nodeOff}, []nodeState{nodeOn, nodeOn}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := treeWithLeafStates(c.states...)
			idx := 0
			if !c.toggleRoot {
				idx = 1
			}
			tr.toggle(idx)

			for i, want := range c.want {
				if got := tr.nodes[i+1].state; got != want {
					t.Errorf("leaf %d state = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestToggleClearsNote(t *testing.T) {
	// A leaf carrying a preselection note loses it the moment the user toggles it,
	// both when toggled directly and via its parent branch.
	leafTree := func() tree {
		tr := tree{}
		root := tr.add(treeNode{label: "root", parent: -1, expanded: true})
		tr.add(treeNode{label: "leaf", parent: root, leaf: true, state: nodePartial, note: "new"})
		return tr
	}

	direct := leafTree()
	direct.toggle(1)
	if direct.nodes[1].note != "" {
		t.Errorf("toggling the leaf left note = %q, want cleared", direct.nodes[1].note)
	}

	viaParent := leafTree()
	viaParent.toggle(0)
	if viaParent.nodes[1].note != "" {
		t.Errorf("toggling the branch left note = %q, want cleared", viaParent.nodes[1].note)
	}
}

func treeWithLeafStates(states ...nodeState) tree {
	tr := tree{}
	root := tr.add(treeNode{label: "root", parent: -1, expanded: true})
	for _, state := range states {
		tr.add(treeNode{label: "leaf", parent: root, leaf: true, state: state})
	}
	return tr
}
