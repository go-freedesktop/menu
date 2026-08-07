// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-freedesktop/desktopentry"
)

// builder holds the shared state for turning a resolved node tree into a
// public [Tree]: the global entry pool, the per-menu selected sets, and the
// set of entries already allocated to a non-OnlyUnallocated menu.
type builder struct {
	pool      []*desktopentry.Entry
	current   string
	selected  map[*node][]*desktopentry.Entry
	allocated map[*desktopentry.Entry]bool
}

// buildTree scans every application directory referenced anywhere in the
// tree once (so allocation compares identical entry values), selects each
// menu's entries, resolves the two-phase OnlyUnallocated allocation, and
// produces the public tree with directories resolved and layout applied.
func buildTree(root *node, current string) *Tree {
	b := &builder{
		current:   current,
		selected:  map[*node][]*desktopentry.Entry{},
		allocated: map[*desktopentry.Entry]bool{},
	}

	var appDirs []string
	collectAppDirs(root, &appDirs)
	// desktopentry.ScanDirs treats its argument as increasing precedence;
	// the XDG lists are highest-first, so reverse before scanning.
	b.pool = desktopentry.ScanDirs(reversed(dedup(appDirs)))

	b.selectAll(root, nil)
	m := b.makeMenu(root, nil)
	return &Tree{Root: m}
}

// selectAll computes, for every menu, the entries its Include/Exclude
// clauses pick from the pool that its accumulated AppDirs make visible; it
// marks the entries of non-OnlyUnallocated menus as allocated.
func (b *builder) selectAll(n *node, accApp []string) {
	acc := concat(accApp, n.appDirs)
	pool := b.visiblePool(acc)
	sel := selectEntries(pool, n.clauses)
	b.selected[n] = sel
	if !n.onlyUnalloc {
		for _, e := range sel {
			b.allocated[e] = true
		}
	}
	for _, c := range n.children {
		b.selectAll(c, acc)
	}
}

// visiblePool returns the pool entries whose file lives under one of dirs and
// that should be shown in the current desktop environment.
func (b *builder) visiblePool(dirs []string) []*desktopentry.Entry {
	var out []*desktopentry.Entry
	for _, e := range b.pool {
		if e.ShouldShowIn(b.current) && underAny(e.Path, dirs) {
			out = append(out, e)
		}
	}
	return out
}

// makeMenu builds the public Menu for n: it resolves the .directory file,
// determines the app list (subtracting allocated entries for an
// OnlyUnallocated menu), builds and prunes the submenus, and orders both
// lists per the menu's layout.
func (b *builder) makeMenu(n *node, accDir []string) *Menu {
	dirs := concat(accDir, n.dirDirs)
	m := &Menu{Name: n.name}
	m.DirectoryName, m.Icon, m.Comment = resolveDirectory(n.directories, dirs)
	if m.DirectoryName == "" {
		m.DirectoryName = n.name
	}

	apps := b.selected[n]
	if n.onlyUnalloc {
		apps = b.unallocatedOnly(apps)
	}

	var subs []*Menu
	for _, c := range n.children {
		sub := b.makeMenu(c, dirs)
		if len(sub.Apps) == 0 && len(sub.Submenus) == 0 {
			continue // prune empty menus
		}
		subs = append(subs, sub)
	}

	lay := n.layout
	if lay == nil {
		lay = n.defLayout
	}
	m.Submenus, m.Apps = orderContents(subs, apps, lay)
	return m
}

// unallocatedOnly keeps only the entries not allocated to a normal menu.
func (b *builder) unallocatedOnly(sel []*desktopentry.Entry) []*desktopentry.Entry {
	var out []*desktopentry.Entry
	for _, e := range sel {
		if !b.allocated[e] {
			out = append(out, e)
		}
	}
	return out
}

// resolveDirectory finds the last <Directory> value that names an existing
// .directory file across dirs (highest precedence first) and returns its
// Name, Icon and Comment. It returns empty strings when none resolves.
func resolveDirectory(names, dirs []string) (name, icon, comment string) {
	for i := len(names) - 1; i >= 0; i-- {
		for _, d := range dirs {
			path := filepath.Join(d, names[i])
			if !fileExists(path) {
				continue
			}
			e, err := desktopentry.ParseFile(path)
			if err != nil {
				continue
			}
			return e.Name, e.Icon, e.Comment
		}
	}
	return "", "", ""
}

// orderContents orders the submenus and apps according to lay. With no
// layout the default applies: submenus first (alphabetical by display name),
// then apps (alphabetical by display name). A layout names specific menus or
// files and uses <Merge> to place the rest.
func orderContents(subs []*Menu, apps []*desktopentry.Entry, lay *layout) ([]*Menu, []*desktopentry.Entry) {
	if lay == nil || len(lay.items) == 0 {
		return sortMenus(subs), sortApps(apps)
	}

	placedMenu := map[*Menu]bool{}
	placedApp := map[*desktopentry.Entry]bool{}
	var outMenus []*Menu
	var outApps []*desktopentry.Entry

	for _, it := range lay.items {
		switch it.kind {
		case layMenuname:
			if s := findMenu(subs, it.value); s != nil && !placedMenu[s] {
				placedMenu[s] = true
				outMenus = append(outMenus, s)
			}
		case layFilename:
			if a := findApp(apps, it.value); a != nil && !placedApp[a] {
				placedApp[a] = true
				outApps = append(outApps, a)
			}
		case layMerge:
			if it.value == "menus" || it.value == "all" {
				for _, s := range sortMenus(subs) {
					if !placedMenu[s] {
						placedMenu[s] = true
						outMenus = append(outMenus, s)
					}
				}
			}
			if it.value == "files" || it.value == "all" {
				for _, a := range sortApps(apps) {
					if !placedApp[a] {
						placedApp[a] = true
						outApps = append(outApps, a)
					}
				}
			}
		default: // laySeparator: no effect on the two split lists
		}
	}
	return outMenus, outApps
}

func findMenu(subs []*Menu, name string) *Menu {
	for _, s := range subs {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func findApp(apps []*desktopentry.Entry, value string) *desktopentry.Entry {
	for _, a := range apps {
		if layoutFilenameMatches(value, a) {
			return a
		}
	}
	return nil
}

func sortMenus(subs []*Menu) []*Menu {
	out := append([]*Menu(nil), subs...)
	sort.SliceStable(out, func(i, j int) bool {
		return menuKey(out[i]) < menuKey(out[j])
	})
	return out
}

func sortApps(apps []*desktopentry.Entry) []*desktopentry.Entry {
	out := append([]*desktopentry.Entry(nil), apps...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func menuKey(m *Menu) string { return strings.ToLower(m.DirectoryName) }

// collectAppDirs gathers every AppDir referenced in the tree, preserving
// first-seen order.
func collectAppDirs(n *node, out *[]string) {
	*out = append(*out, n.appDirs...)
	for _, c := range n.children {
		collectAppDirs(c, out)
	}
}

// underAny reports whether path is equal to or nested under one of dirs.
func underAny(path string, dirs []string) bool {
	cp := filepath.Clean(path)
	for _, d := range dirs {
		cd := filepath.Clean(d)
		if cp == cd || strings.HasPrefix(cp, cd+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func concat(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	return append(out, b...)
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func reversed(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
