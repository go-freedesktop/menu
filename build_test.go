// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	"github.com/go-freedesktop/desktopentry"
)

func TestRuleMatch(t *testing.T) {
	e := &desktopentry.Entry{ID: "app", Categories: []string{"Utility"}}
	cases := []struct {
		name string
		r    rule
		want bool
	}{
		{"all", rule{op: opAll}, true},
		{"cat-hit", rule{op: opCategory, value: "Utility"}, true},
		{"cat-miss", rule{op: opCategory, value: "System"}, false},
		{"file-hit", rule{op: opFilename, value: "app.desktop"}, true},
		{"file-miss", rule{op: opFilename, value: "other.desktop"}, false},
		{"and-true", rule{op: opAnd, subs: []rule{{op: opAll}, {op: opCategory, value: "Utility"}}}, true},
		{"and-false", rule{op: opAnd, subs: []rule{{op: opAll}, {op: opCategory, value: "System"}}}, false},
		{"or-true", rule{op: opOr, subs: []rule{{op: opCategory, value: "System"}, {op: opAll}}}, true},
		{"or-false", rule{op: opOr, subs: []rule{{op: opCategory, value: "System"}}}, false},
		{"not-true", rule{op: opNot, subs: []rule{{op: opCategory, value: "System"}}}, true},
		{"not-false", rule{op: opNot, subs: []rule{{op: opCategory, value: "Utility"}}}, false},
		{"none", rule{op: opNone}, false},
	}
	for _, c := range cases {
		if got := c.r.match(e); got != c.want {
			t.Errorf("%s: match = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAbsorbCopiesLayouts(t *testing.T) {
	dst := &node{name: "X"}
	lay := &layout{}
	def := &layout{}
	src := &node{name: "X", layout: lay, defLayout: def, onlyUnalloc: true, deleted: true}
	absorb(dst, src)
	if dst.layout != lay || dst.defLayout != def {
		t.Error("absorb did not copy layout/defLayout")
	}
	if !dst.onlyUnalloc || !dst.deleted {
		t.Error("absorb did not adopt boolean flags")
	}

	// When the later node has no layouts, the earlier ones are kept.
	keep := &layout{}
	dst2 := &node{layout: keep, defLayout: keep}
	absorb(dst2, &node{})
	if dst2.layout != keep || dst2.defLayout != keep {
		t.Error("absorb overwrote existing layouts with nil")
	}
}

func TestMergeContentLayouts(t *testing.T) {
	dst := &node{}
	lay, def := &layout{}, &layout{}
	mergeContent(dst, &node{layout: lay, defLayout: def})
	if dst.layout != lay || dst.defLayout != def {
		t.Error("mergeContent did not adopt layouts into empty dst")
	}
	keep, keepDef := &layout{}, &layout{}
	dst2 := &node{layout: keep, defLayout: keepDef}
	mergeContent(dst2, &node{layout: &layout{}, defLayout: &layout{}})
	if dst2.layout != keep || dst2.defLayout != keepDef {
		t.Error("mergeContent overwrote existing layouts")
	}
}

func TestApplyMovesAndPaths(t *testing.T) {
	// Missing source path: the move is a silent no-op.
	root := &node{name: "R", moves: []moveOp{{old: "Nope", new: "Dst"}}}
	applyMoves(root)
	if len(root.children) != 0 {
		t.Error("no-op move should not create children")
	}

	// findPath on a missing intermediate returns (parent, nil).
	parent, target := findPath(root, "a/b")
	if parent != root || target != nil {
		t.Errorf("findPath missing = (%v,%v)", parent, target)
	}

	// ensurePath creates intermediate menus; a real move merges into it.
	root2 := &node{name: "R", children: []*node{{name: "Src", clauses: []clause{{include: true, rule: rule{op: opAll}}}}}}
	root2.moves = []moveOp{{old: "Src", new: "Group/Dst"}}
	applyMoves(root2)
	grp := childByName(root2, "Group")
	if grp == nil || childByName(grp, "Dst") == nil {
		t.Fatalf("move did not build Group/Dst: %+v", root2.children)
	}
	if childByName(root2, "Src") != nil {
		t.Error("source menu should have been removed")
	}
	if len(childByName(grp, "Dst").clauses) != 1 {
		t.Error("moved clauses lost")
	}
}

func TestRemoveChildNilParent(t *testing.T) {
	removeChild(nil, &node{}) // must not panic
}

func TestResolveDirectory(t *testing.T) {
	dirs := []string{"testdata/desktop-directories"}
	// Last existing directory file wins; a missing one is skipped.
	name, icon, comment := resolveDirectory([]string{"Missing.directory", "Applications.directory"}, dirs)
	if name != "All Applications" || icon != "applications-all" || comment == "" {
		t.Errorf("resolveDirectory = %q/%q/%q", name, icon, comment)
	}
	// None resolve.
	if n, _, _ := resolveDirectory([]string{"Missing.directory"}, dirs); n != "" {
		t.Errorf("expected empty, got %q", n)
	}
	// A file that exists but fails to parse is skipped (parse-error branch).
	if n, _, _ := resolveDirectory([]string{"Broken.directory"}, dirs); n != "" {
		t.Errorf("broken directory should not resolve, got %q", n)
	}
}

func TestOrderContentsDefault(t *testing.T) {
	subs := []*Menu{{Name: "b", DirectoryName: "Beta"}, {Name: "a", DirectoryName: "Alpha"}}
	apps := []*desktopentry.Entry{{ID: "z", Name: "Zed"}, {ID: "a", Name: "Ada"}}
	gotSubs, gotApps := orderContents(subs, apps, nil)
	if gotSubs[0].DirectoryName != "Alpha" || gotApps[0].Name != "Ada" {
		t.Errorf("default ordering wrong: %v / %v", gotSubs, gotApps)
	}
	// Empty layout falls back to the default order too.
	gotSubs2, _ := orderContents(subs, apps, &layout{})
	if gotSubs2[0].DirectoryName != "Alpha" {
		t.Error("empty layout should use default order")
	}
}

func TestOrderContentsLayout(t *testing.T) {
	subs := []*Menu{{Name: "Games", DirectoryName: "Games"}, {Name: "Sys", DirectoryName: "System"}}
	apps := []*desktopentry.Entry{{ID: "ed", Name: "Editor"}, {ID: "tm", Name: "Term"}}
	lay := &layout{items: []layoutEntry{
		{kind: layMenuname, value: "Sys"},   // pin System first
		{kind: layMenuname, value: "Ghost"}, // not found
		{kind: layFilename, value: "tm"},    // pin Term first (bare id)
		{kind: layFilename, value: "ghost"}, // not found
		{kind: laySeparator},                // ignored
		{kind: layMerge, value: "menus"},    // rest of the menus
		{kind: layMerge, value: "files"},    // rest of the apps
	}}
	gotSubs, gotApps := orderContents(subs, apps, lay)
	if gotSubs[0].Name != "Sys" || gotSubs[1].Name != "Games" {
		t.Errorf("layout submenu order = %v", []string{gotSubs[0].Name, gotSubs[1].Name})
	}
	if gotApps[0].Name != "Term" || gotApps[1].Name != "Editor" {
		t.Errorf("layout app order = %v", []string{gotApps[0].Name, gotApps[1].Name})
	}

	// A Merge type="all" places both remaining menus and files.
	layAll := &layout{items: []layoutEntry{{kind: layMerge, value: "all"}}}
	s, a := orderContents(subs, apps, layAll)
	if len(s) != 2 || len(a) != 2 {
		t.Errorf("merge all: subs=%d apps=%d", len(s), len(a))
	}
}

func TestUnderAny(t *testing.T) {
	if !underAny("/a/b", []string{"/a/b"}) {
		t.Error("exact path should match")
	}
	if !underAny("/a/b/c.desktop", []string{"/a/b"}) {
		t.Error("nested path should match")
	}
	if underAny("/x/y", []string{"/a", "/b"}) {
		t.Error("unrelated path should not match")
	}
}

func TestHelpers(t *testing.T) {
	if got := reversed([]string{"a", "b", "c"}); got[0] != "c" || got[2] != "a" {
		t.Errorf("reversed = %v", got)
	}
	if got := dedup([]string{"a", "a", "b"}); len(got) != 2 {
		t.Errorf("dedup = %v", got)
	}
	if got := concat([]string{"a"}, []string{"b"}); len(got) != 2 {
		t.Errorf("concat = %v", got)
	}
	if got := splitPath("/a//b/"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitPath = %v", got)
	}
}

// TestLoadSuccess plants a menu file where the XDG resolver will find it, so
// Load() runs end to end (covering the XDG default-directory builders).
func TestLoadSuccess(t *testing.T) {
	cfg := t.TempDir()
	data := t.TempDir()
	menusDir := filepath.Join(cfg, "menus")
	if err := os.MkdirAll(menusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appsDir := filepath.Join(data, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(appsDir, "org.example.A.desktop"),
		"[Desktop Entry]\nType=Application\nName=A\nExec=a\nCategories=Utility;\n")
	writeFile(t, filepath.Join(menusDir, "applications.menu"), `<Menu>
	  <Name>Applications</Name>
	  <DefaultAppDirs/>
	  <DefaultDirectoryDirs/>
	  <DefaultMergeDirs/>
	  <Menu><Name>Acc</Name><Include><Category>Utility</Category></Include></Menu>
	</Menu>`)

	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(t.TempDir(), "etc"))
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(t.TempDir(), "share"))
	t.Setenv("XDG_CURRENT_DESKTOP", "")
	xdg.Reload()

	tree, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The XDG application directories differ across platforms (darwin adds
	// extra locations), so only assert that Load resolved the planted menu
	// file and produced its root; the allocation logic is asserted in detail
	// by the directory-injectable TestLoadWithDirs.
	if tree.Root == nil || tree.Root.Name != "Applications" {
		t.Errorf("Load root wrong: %+v", tree.Root)
	}
}
