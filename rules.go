// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import (
	"github.com/go-freedesktop/desktopentry"
)

// ruleOp identifies the kind of a match rule.
type ruleOp int

const (
	opNone     ruleOp = iota // matches nothing (unknown element)
	opAll                    // <All/>: matches every entry
	opCategory               // <Category>: matches entries in a category
	opFilename               // <Filename>: matches one desktop-file id
	opAnd                    // <And>: all sub-rules match
	opOr                     // <Or>/<Include>/<Exclude>: any sub-rule matches
	opNot                    // <Not>: no sub-rule matches
)

// rule is a boolean expression over a desktop entry, mirroring the match
// elements of the Desktop Menu Specification.
type rule struct {
	op    ruleOp
	value string // category name or desktop-file id
	subs  []rule // operands of And/Or/Not
}

// match reports whether e satisfies the rule.
func (r rule) match(e *desktopentry.Entry) bool {
	switch r.op {
	case opAll:
		return true
	case opCategory:
		for _, c := range e.Categories {
			if c == r.value {
				return true
			}
		}
		return false
	case opFilename:
		return desktopFileID(e) == r.value
	case opAnd:
		for _, s := range r.subs {
			if !s.match(e) {
				return false
			}
		}
		return true
	case opOr:
		for _, s := range r.subs {
			if s.match(e) {
				return true
			}
		}
		return false
	case opNot:
		for _, s := range r.subs {
			if s.match(e) {
				return false
			}
		}
		return true
	default: // opNone
		return false
	}
}

// desktopFileID returns the entry's desktop-file id with the ".desktop"
// suffix, the form <Filename> rules are written against (e.g. "kde4-konsole"
// becomes "kde4-konsole.desktop").
func desktopFileID(e *desktopentry.Entry) string {
	if e.ID == "" {
		return ""
	}
	return e.ID + ".desktop"
}

// selectEntries applies a menu's ordered <Include>/<Exclude> clauses to the
// candidate pool: an <Include> adds every matching entry, an <Exclude>
// removes every matching entry, evaluated in document order. The result
// preserves the pool's order.
func selectEntries(pool []*desktopentry.Entry, clauses []clause) []*desktopentry.Entry {
	in := map[*desktopentry.Entry]bool{}
	for _, c := range clauses {
		for _, e := range pool {
			if c.rule.match(e) {
				in[e] = c.include
			}
		}
	}
	var out []*desktopentry.Entry
	for _, e := range pool {
		if in[e] {
			out = append(out, e)
		}
	}
	return out
}

// layoutKind identifies a layout entry.
type layoutKind int

const (
	layMenuname  layoutKind = iota // <Menuname>: place a named submenu
	layFilename                    // <Filename>: place a named app
	laySeparator                   // <Separator>: a visual break (ignored here)
	layMerge                       // <Merge type="menus|files|all">
)

// layoutEntry is one ordered item of a <Layout>/<DefaultLayout>.
type layoutEntry struct {
	kind  layoutKind
	value string // name/id, or the Merge type
}

// layout is an ordered display specification for a menu's contents.
type layout struct {
	items []layoutEntry
}

// layoutFilenameMatches reports whether the layout <Filename> value refers
// to entry e, accepting the id both with and without the ".desktop" suffix.
func layoutFilenameMatches(value string, e *desktopentry.Entry) bool {
	return value == desktopFileID(e) || value == e.ID
}
