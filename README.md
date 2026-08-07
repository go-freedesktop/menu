# menu — go-freedesktop

[![ci](https://github.com/go-freedesktop/menu/actions/workflows/ci.yml/badge.svg)](https://github.com/go-freedesktop/menu/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-freedesktop/menu.svg)](https://pkg.go.dev/github.com/go-freedesktop/menu)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

The freedesktop **[Desktop Menu Specification](https://specifications.freedesktop.org/menu-spec/latest/)**
for a launcher — the piece that turns the installed applications into the
**categorized category menu** a launcher shows (Accessories, Graphics, System,
Games, …). It reads the `applications.menu` XML file and produces a resolved
tree of menus, each with its display name, icon, sub-menus and application
entries. Pure Go, **CGO-free**.

## Scope — what this adds, and what it reuses

This is **Wave 2** of go-freedesktop; it stands on Wave 1 rather than
reimplementing it:

- **[`github.com/go-freedesktop/desktopentry`](https://github.com/go-freedesktop/desktopentry)**
  (BSD-3) — enumerates and parses the `.desktop` entries (`ScanDirs`,
  `Entry`, `ShouldShowIn`). This package does **not** re-implement `.desktop`
  parsing.
- **[`github.com/adrg/xdg`](https://github.com/adrg/xdg)** (MIT) — XDG
  base-directory resolution.

On top of those it implements the **menu-spec algorithm**:

- parse the `<Menu>` tree of an `applications.menu` file: `<Name>`,
  `<Directory>`, `<Include>`/`<Exclude>` with the
  `<And>`/`<Or>`/`<Not>`/`<Category>`/`<Filename>`/`<All>` match rules,
  `<AppDir>`/`<DefaultAppDirs>`, `<DirectoryDir>`/`<DefaultDirectoryDirs>`,
  `<MergeFile>`/`<MergeDir>`/`<DefaultMergeDirs>`, `<OnlyUnallocated>`,
  `<Deleted>`, `<Move>` and `<Layout>`/`<DefaultLayout>`;
- **merge** referenced menu files (with cycle protection), **consolidate**
  menus that share a `<Name>`, apply `<Move>` rules and drop `<Deleted>`
  menus, per the spec;
- **resolve** the include/exclude rule expressions against the entries
  scanned by `desktopentry.ScanDirs`, honoring the two-phase
  **`<OnlyUnallocated>`** allocation (an unallocated "Other" menu gets exactly
  what no normal menu claimed);
- **resolve** each menu's `<Directory>` `.directory` file for its display
  name, icon and comment;
- **order** each menu's contents with its `<Layout>`/`<DefaultLayout>`
  (`<Menuname>`, `<Filename>`, `<Merge type="menus|files|all">`,
  `<Separator>`), defaulting to alphabetical sub-menus then applications.

## Install

```sh
go get github.com/go-freedesktop/menu
```

## Quickstart

```go
package main

import (
	"fmt"

	"github.com/go-freedesktop/menu"
)

func main() {
	tree, err := menu.Load() // reads the standard XDG applications.menu
	if err != nil {
		panic(err)
	}
	walk(tree.Root, 0)
}

func walk(m *menu.Menu, depth int) {
	fmt.Printf("%*s%s\n", depth*2, "", m.DirectoryName)
	for _, a := range m.Apps {
		fmt.Printf("%*s- %s  (%s)\n", depth*2+2, "", a.Name, a.Icon)
	}
	for _, sub := range m.Submenus {
		walk(sub, depth+1)
	}
}
```

## Public API

| Symbol | Purpose |
| --- | --- |
| `Load() (*Tree, error)` | build the menu from the standard XDG locations |
| `LoadWithDirs(menuFile string, appDirs, dirDirs, mergeDirs []string, current string) (*Tree, error)` | directory-injectable form (tests / custom search paths) |
| `Tree` | the resolved menu; `Tree.Root` is the top-level `*Menu` |
| `Menu` | one node: `Name`, `DirectoryName`, `Icon`, `Comment`, `Submenus []*Menu`, `Apps []*desktopentry.Entry` |

`current` is the desktop-environment name (as in `XDG_CURRENT_DESKTOP`) used
for `OnlyShowIn`/`NotShowIn` filtering; the empty string shows every entry.

## wasmdesk integration

This is the **application-menu** layer of the wasmdesk launcher:

- `Load()` → the whole category menu (Accessories / Graphics / System / …),
  ready to render as a nested pop-up or a sidebar tree.
- each `Menu.Apps` entry is a `*desktopentry.Entry`, so a click flows straight
  into `desktopentry.ExpandExec` to launch it.
- each `Menu.Icon` and each entry's `Icon` feed
  [`go-freedesktop/icontheme`](https://github.com/go-freedesktop/icontheme)
  to resolve a concrete image path.

Together with `desktopentry` (what to launch) and `icontheme` (how to draw
it), this completes the three freedesktop layers a launcher needs.

## Tests & coverage

`CGO_ENABLED=0 go test ./...` — **100% statement coverage**, including every
error branch, driven by a synthetic `applications.menu` plus an app/directory
fixture tree under `testdata/` that exercises include/exclude, And/Or/Not,
merge (file + dir + default), same-name consolidation, `<Move>`, `<Deleted>`,
`<Layout>` and `<OnlyUnallocated>` allocation. CI additionally cross-builds
and runs the suite on the six supported 64-bit targets (amd64/arm64 natively,
riscv64/loong64/ppc64le/s390x under qemu-user).

## License

BSD-3-Clause. Copyright (c) the go-freedesktop/menu authors.
