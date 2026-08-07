// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

// Package menu implements the freedesktop.org Desktop Menu Specification:
// it reads the applications.menu XML file and turns it into a categorized
// tree of installed applications — the "category menu" an application
// launcher shows (Accessories, Graphics, System, ...).
//
// It deliberately does not reinvent the layers below it. The individual
// .desktop entries are enumerated and parsed by
// github.com/go-freedesktop/desktopentry (its [desktopentry.ScanDirs] and
// [desktopentry.Entry]), and the XDG base-directory resolution is delegated
// to github.com/adrg/xdg. On top of those this package adds the menu-spec
// gap:
//
//   - parse the <Menu> tree of an applications.menu file, including
//     <Include>/<Exclude> match rules (<And>/<Or>/<Not>/<Category>/
//     <Filename>/<All>), <AppDir>/<DefaultAppDirs>, <DirectoryDir>/
//     <DefaultDirectoryDirs>, <MergeFile>/<MergeDir>/<DefaultMergeDirs>,
//     <Directory>, <OnlyUnallocated>, <Deleted>, <Move> and <Layout>;
//   - merge referenced menu files, consolidate menus sharing a <Name>,
//     apply <Move> rules and drop <Deleted> menus, per the spec;
//   - resolve the include/exclude rule expressions against the scanned
//     desktop entries, honoring the two-phase <OnlyUnallocated> allocation;
//   - resolve each menu's <Directory> .directory file for its display name
//     and icon;
//   - order the result with the menu's <Layout>/<DefaultLayout>.
//
// The result is a [Tree] of [Menu] nodes, each exposing its display Name,
// Icon and the ordered lists of Submenus and Apps.
package menu

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
	"github.com/go-freedesktop/desktopentry"
)

// Menu is one node of the resolved application menu.
type Menu struct {
	// Name is the menu's <Name>, the identifier used by <Move>, <Layout>
	// and same-name consolidation. It is never localized.
	Name string
	// DirectoryName is the user-visible label: the Name key of the menu's
	// resolved <Directory> .directory file, or Name when there is none.
	DirectoryName string
	// Icon is the icon name/path from the .directory file (feeds an
	// icon-theme lookup), empty when unset.
	Icon string
	// Comment is the localized description from the .directory file.
	Comment string
	// Submenus are the child menus, ordered per the menu's layout.
	Submenus []*Menu
	// Apps are the application entries allocated to this menu, ordered per
	// the menu's layout.
	Apps []*desktopentry.Entry
}

// Tree is a fully resolved application menu.
type Tree struct {
	// Root is the top-level menu (typically named "Applications").
	Root *Menu
}

// Load builds the application menu from the standard locations: the
// applications.menu file resolved through the XDG config directories
// (with the $XDG_CONFIG_HOME copy overriding the system one), the
// applications/ directories, the desktop-directories/ directories and the
// menus/applications-merged/ merge directories, all provided by
// github.com/adrg/xdg. Entry visibility honors $XDG_CURRENT_DESKTOP.
func Load() (*Tree, error) {
	menuFile, err := xdg.SearchConfigFile(filepath.Join("menus", "applications.menu"))
	if err != nil {
		return nil, err
	}

	ctx := &parseCtx{
		appDirs:     defaultAppDirs(),
		dirDirs:     defaultDirectoryDirs(),
		mergeDirs:   defaultMergeDirs(),
		parentFiles: parentMenuFiles(menuFile),
		visited:     map[string]bool{},
	}
	return loadWithCtx(menuFile, ctx, os.Getenv("XDG_CURRENT_DESKTOP"))
}

// LoadWithDirs is the directory-injectable form of [Load], intended for
// tests and for embedders that manage their own search paths. menuFile is
// the applications.menu file to read; appDirs, dirDirs and mergeDirs supply
// the concrete directories that the XML's <DefaultAppDirs>,
// <DefaultDirectoryDirs> and <DefaultMergeDirs> elements expand to; current
// is the desktop environment name used for OnlyShowIn/NotShowIn filtering
// (empty means "show everything").
func LoadWithDirs(menuFile string, appDirs, dirDirs, mergeDirs []string, current string) (*Tree, error) {
	ctx := &parseCtx{
		appDirs:   appDirs,
		dirDirs:   dirDirs,
		mergeDirs: mergeDirs,
		visited:   map[string]bool{},
	}
	return loadWithCtx(menuFile, ctx, current)
}

// loadWithCtx is the shared implementation: parse the menu file, resolve the
// merges/moves/consolidation, then build the allocated tree.
func loadWithCtx(menuFile string, ctx *parseCtx, current string) (*Tree, error) {
	root, err := parseMenuFile(menuFile, ctx)
	if err != nil {
		return nil, err
	}
	resolveNode(root)
	return buildTree(root, current), nil
}

// defaultAppDirs returns the applications/ directories, highest precedence
// first (as github.com/adrg/xdg orders them).
func defaultAppDirs() []string { return xdg.ApplicationDirs }

// defaultDirectoryDirs returns the desktop-directories/ directories,
// $XDG_DATA_HOME first then each $XDG_DATA_DIRS entry.
func defaultDirectoryDirs() []string {
	dirs := []string{filepath.Join(xdg.DataHome, "desktop-directories")}
	for _, d := range xdg.DataDirs {
		dirs = append(dirs, filepath.Join(d, "desktop-directories"))
	}
	return dirs
}

// defaultMergeDirs returns the menus/applications-merged/ directories,
// $XDG_CONFIG_HOME first then each $XDG_CONFIG_DIRS entry.
func defaultMergeDirs() []string {
	dirs := []string{filepath.Join(xdg.ConfigHome, "menus", "applications-merged")}
	for _, d := range xdg.ConfigDirs {
		dirs = append(dirs, filepath.Join(d, "menus", "applications-merged"))
	}
	return dirs
}

// parentMenuFiles returns the applications.menu files found in the config
// directories other than the chosen one, lower precedence first; these feed
// <MergeFile type="parent">.
func parentMenuFiles(chosen string) []string {
	var out []string
	all := append([]string{xdg.ConfigHome}, xdg.ConfigDirs...)
	// Lower precedence first: reverse the highest-first XDG order.
	for i := len(all) - 1; i >= 0; i-- {
		p := filepath.Join(all[i], "menus", "applications.menu")
		if p != chosen {
			out = append(out, p)
		}
	}
	return out
}
