// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/go-freedesktop/desktopentry"
)

const (
	menuFile = "testdata/menus/applications.menu"
)

func testDirs() (appDirs, dirDirs, mergeDirs []string) {
	return []string{"testdata/applications"},
		[]string{"testdata/desktop-directories"},
		[]string{"testdata/menus/applications-merged"}
}

// submenuNames returns the ordered DirectoryNames of a menu's submenus.
func submenuNames(m *Menu) []string {
	var out []string
	for _, s := range m.Submenus {
		out = append(out, s.DirectoryName)
	}
	return out
}

// appNames returns the ordered application Names of a menu.
func appNames(m *Menu) []string {
	var out []string
	for _, a := range m.Apps {
		out = append(out, a.Name)
	}
	return out
}

// findSub returns the submenu with the given internal Name.
func findSub(m *Menu, name string) *Menu {
	for _, s := range m.Submenus {
		if s.Name == name {
			return s
		}
	}
	return nil
}

func TestLoadWithDirs(t *testing.T) {
	appDirs, dirDirs, mergeDirs := testDirs()
	tree, err := LoadWithDirs(menuFile, appDirs, dirDirs, mergeDirs, "")
	if err != nil {
		t.Fatalf("LoadWithDirs: %v", err)
	}
	root := tree.Root

	// Root <Directory>: the last existing one wins (Missing.directory is
	// skipped, Applications.directory resolves).
	if root.DirectoryName != "All Applications" {
		t.Errorf("root DirectoryName = %q, want All Applications", root.DirectoryName)
	}
	if root.Icon != "applications-all" {
		t.Errorf("root Icon = %q, want applications-all", root.Icon)
	}
	if root.Comment != "Every installed application" {
		t.Errorf("root Comment = %q", root.Comment)
	}

	// Submenu order is set by the root <DefaultLayout>: Accessories pinned
	// first (Menuname), then the rest merged alphabetically by DirectoryName.
	// Empty is pruned; Dead is deleted; SysTemp was moved to System.
	got := submenuNames(root)
	want := []string{"Accessories", "Games", "Graphics", "Network", "Other", "System"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("submenu order = %v, want %v", got, want)
	}

	// Accessories: Utility AND NOT System. With current="" the OnlyShowIn
	// entry is visible, so both Editor and the GNOME-only utility appear.
	acc := findSub(root, "Accessories")
	if acc.DirectoryName != "Accessories" || acc.Icon != "applications-utilities" {
		t.Errorf("Accessories dir/icon = %q/%q", acc.DirectoryName, acc.Icon)
	}
	if strings.Join(appNames(acc), ",") != "Gnome Utility,Text Editor" {
		t.Errorf("Accessories apps = %v", appNames(acc))
	}

	// Graphics: (Graphics OR Filename Paint) minus the Game exclude. Its
	// <Layout> pins Paint first (a non-existent Filename is skipped).
	gfx := findSub(root, "Graphics")
	if strings.Join(appNames(gfx), ",") != "Paint" {
		t.Errorf("Graphics apps = %v", appNames(gfx))
	}

	// Games: the main <Category>Game plus the Filename added by the merged
	// applications-merged/extra.menu fragment (consolidated by name).
	games := findSub(root, "Games")
	if strings.Join(appNames(games), ",") != "Alien Game,Board" {
		t.Errorf("Games apps = %v", appNames(games))
	}

	// System: created by <Move> from SysTemp; System category entry.
	sys := findSub(root, "System")
	if strings.Join(appNames(sys), ",") != "Terminal" {
		t.Errorf("System apps = %v", appNames(sys))
	}

	// Network: contributed by the explicit <MergeFile>inc.menu.
	net := findSub(root, "Network")
	if net == nil || strings.Join(appNames(net), ",") != "Net" {
		t.Errorf("Network apps = %v", appNames(net))
	}
	// Network.directory does not exist, so DirectoryName falls back to Name.
	if net.DirectoryName != "Network" {
		t.Errorf("Network DirectoryName = %q", net.DirectoryName)
	}

	// Other: OnlyUnallocated + <All> -> only the entry no normal menu took.
	other := findSub(root, "Other")
	if strings.Join(appNames(other), ",") != "Misc Office" {
		t.Errorf("Other apps = %v", appNames(other))
	}

	// Empty menu (Filename of a non-existent entry) must be pruned.
	if findSub(root, "Empty") != nil {
		t.Error("Empty menu should be pruned")
	}
	// Dead menu is <Deleted/>.
	if findSub(root, "Dead") != nil {
		t.Error("Dead menu should be deleted")
	}
}

func TestLoadWithDirsCurrentDesktopFilters(t *testing.T) {
	appDirs, dirDirs, mergeDirs := testDirs()
	tree, err := LoadWithDirs(menuFile, appDirs, dirDirs, mergeDirs, "KDE")
	if err != nil {
		t.Fatalf("LoadWithDirs: %v", err)
	}
	acc := findSub(tree.Root, "Accessories")
	// The GNOME-only utility is filtered out under KDE.
	if strings.Join(appNames(acc), ",") != "Text Editor" {
		t.Errorf("Accessories under KDE = %v, want [Text Editor]", appNames(acc))
	}
	// It must not leak into the unallocated Other menu either.
	other := findSub(tree.Root, "Other")
	for _, a := range other.Apps {
		if a.Name == "Gnome Utility" {
			t.Error("GNOME-only entry leaked into Other under KDE")
		}
	}
}

func TestLoadMissingMenuFile(t *testing.T) {
	appDirs, dirDirs, mergeDirs := testDirs()
	if _, err := LoadWithDirs("testdata/menus/nope.menu", appDirs, dirDirs, mergeDirs, ""); err == nil {
		t.Fatal("expected error for missing menu file")
	}
}

// TestLoadUsesXDG exercises the zero-config Load() path. There is no menu
// file in the test environment's XDG dirs, so it is expected to return an
// error; the point is to run the XDG-resolution code.
func TestLoadUsesXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", filepath.Join(t.TempDir(), "etc"))
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_DATA_DIRS", filepath.Join(t.TempDir(), "share"))
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	xdg.Reload()
	if _, err := Load(); err == nil {
		t.Log("Load() unexpectedly found a menu file; that is acceptable")
	}
}

func TestDesktopFileID(t *testing.T) {
	if got := desktopFileID(&desktopentry.Entry{ID: "kde4-konsole"}); got != "kde4-konsole.desktop" {
		t.Errorf("desktopFileID = %q", got)
	}
	if got := desktopFileID(&desktopentry.Entry{}); got != "" {
		t.Errorf("empty id desktopFileID = %q, want empty", got)
	}
}
