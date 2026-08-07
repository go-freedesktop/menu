// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu_test

import (
	"fmt"

	"github.com/go-freedesktop/menu"
)

// ExampleLoadWithDirs builds the categorized application menu from an
// injected set of directories (the same shape [menu.Load] uses with the
// standard XDG paths) and prints each top-level category with its resolved
// display name and application count.
func ExampleLoadWithDirs() {
	tree, err := menu.LoadWithDirs(
		"testdata/menus/applications.menu",
		[]string{"testdata/applications"},
		[]string{"testdata/desktop-directories"},
		[]string{"testdata/menus/applications-merged"},
		"", // show entries for every desktop environment
	)
	if err != nil {
		panic(err)
	}

	fmt.Println(tree.Root.DirectoryName)
	for _, m := range tree.Root.Submenus {
		fmt.Printf("  %-12s %d app(s)\n", m.DirectoryName, len(m.Apps))
	}

	// Output:
	// All Applications
	//   Accessories  2 app(s)
	//   Games        2 app(s)
	//   Graphics     1 app(s)
	//   Network      1 app(s)
	//   Other        1 app(s)
	//   System       1 app(s)
}
