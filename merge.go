// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import "strings"

// mergeContent inlines the content of a merged file's root menu (src) into
// the menu that referenced it (dst): the merged children, rule clauses,
// directory/app/directory-dir lists and moves are appended in place, and a
// layout is adopted only when dst has none. The merged root's own <Name>,
// <OnlyUnallocated> and <Deleted> are ignored, per the spec.
func mergeContent(dst, src *node) {
	dst.directories = append(dst.directories, src.directories...)
	dst.appDirs = append(dst.appDirs, src.appDirs...)
	dst.dirDirs = append(dst.dirDirs, src.dirDirs...)
	dst.clauses = append(dst.clauses, src.clauses...)
	dst.children = append(dst.children, src.children...)
	dst.moves = append(dst.moves, src.moves...)
	if dst.layout == nil {
		dst.layout = src.layout
	}
	if dst.defLayout == nil {
		dst.defLayout = src.defLayout
	}
}

// resolveNode turns a parsed node into its final shape: it consolidates
// child menus that share a <Name>, applies the <Move> rules, drops the
// <Deleted> and nameless children, and recurses. The order matches the
// specification: consolidate, then move, then delete.
func resolveNode(n *node) {
	consolidateChildren(n)
	applyMoves(n)

	kept := n.children[:0]
	for _, c := range n.children {
		if c.deleted || c.name == "" {
			continue
		}
		kept = append(kept, c)
	}
	n.children = kept

	for _, c := range n.children {
		resolveNode(c)
	}
}

// consolidateChildren merges every group of same-named child menus into the
// first occurrence, preserving first-seen order. Later menus' boolean flags
// (OnlyUnallocated, Deleted) override earlier ones.
func consolidateChildren(n *node) {
	byName := map[string]*node{}
	out := n.children[:0]
	for _, c := range n.children {
		if first, ok := byName[c.name]; ok && c.name != "" {
			absorb(first, c)
			continue
		}
		byName[c.name] = c
		out = append(out, c)
	}
	n.children = out
}

// absorb folds later (l) into an earlier same-named menu (dst).
func absorb(dst, l *node) {
	dst.directories = append(dst.directories, l.directories...)
	dst.appDirs = append(dst.appDirs, l.appDirs...)
	dst.dirDirs = append(dst.dirDirs, l.dirDirs...)
	dst.clauses = append(dst.clauses, l.clauses...)
	dst.children = append(dst.children, l.children...)
	dst.moves = append(dst.moves, l.moves...)
	dst.onlyUnalloc = l.onlyUnalloc
	dst.deleted = l.deleted
	if l.layout != nil {
		dst.layout = l.layout
	}
	if l.defLayout != nil {
		dst.defLayout = l.defLayout
	}
}

// applyMoves executes n's <Move> operations in order: each moves the submenu
// at the Old path to the New path, creating the destination path if needed
// and merging into an existing destination, then re-consolidating it.
func applyMoves(n *node) {
	for _, mv := range n.moves {
		parent, src := findPath(n, mv.old)
		if src == nil {
			continue
		}
		removeChild(parent, src)
		dst := ensurePath(n, mv.new)
		absorb(dst, src)
		consolidateChildren(dst)
	}
	n.moves = nil
}

// splitPath splits a "/"-separated menu path into its non-empty components.
func splitPath(path string) []string {
	var parts []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

// findPath locates the menu at the "/"-separated path under root, returning
// its immediate parent and the node itself (nil node when not found).
func findPath(root *node, path string) (parent, target *node) {
	cur := root
	for _, part := range splitPath(path) {
		next := childByName(cur, part)
		if next == nil {
			return cur, nil
		}
		parent, cur = cur, next
	}
	return parent, cur
}

// ensurePath returns the menu at path under root, creating any missing
// intermediate menus.
func ensurePath(root *node, path string) *node {
	cur := root
	for _, part := range splitPath(path) {
		next := childByName(cur, part)
		if next == nil {
			next = &node{name: part}
			cur.children = append(cur.children, next)
		}
		cur = next
	}
	return cur
}

// childByName returns the first child of n named name, or nil.
func childByName(n *node, name string) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

// removeChild detaches c from parent's children (no-op if not present).
func removeChild(parent, c *node) {
	if parent == nil {
		return
	}
	out := parent.children[:0]
	for _, ch := range parent.children {
		if ch != c {
			out = append(out, ch)
		}
	}
	parent.children = out
}
