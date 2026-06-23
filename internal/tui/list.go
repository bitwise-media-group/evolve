// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package tui

// listItem is one row in a flat selection list (filters, harnesses, models). Its
// display state (checked, disabled, annotation) is derived from the plan.Session
// at render time, so only its stable identity lives here. A header row groups the
// rows beneath it (the models list uses one per provider); toggling it selects or
// deselects the whole group rather than a single unit.
type listItem struct {
	label  string
	id     string // filter key / harness id / model key
	header bool   // group header row (e.g. a provider); toggles the whole group
	group  string // header rows -> the group id (provider id) they own
}

// list is a flat, navigable selection list; selection state is owned by the
// Session, not stored here.
type list struct {
	items  []listItem
	cursor int
}

func (l *list) move(delta int) {
	if len(l.items) == 0 {
		return
	}
	l.cursor += delta
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor >= len(l.items) {
		l.cursor = len(l.items) - 1
	}
}

func (l *list) top() { l.cursor = 0 }
func (l *list) bottom() {
	if len(l.items) > 0 {
		l.cursor = len(l.items) - 1
	}
}

// current returns the item under the cursor, or false when the list is empty.
func (l *list) current() (listItem, bool) {
	if l.cursor < 0 || l.cursor >= len(l.items) {
		return listItem{}, false
	}
	return l.items[l.cursor], true
}
