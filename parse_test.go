// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newCtx() *parseCtx { return &parseCtx{visited: map[string]bool{}} }

// parse is a test helper: parse a menu document from a string.
func parse(t *testing.T, doc string, ctx *parseCtx, baseDir string) *node {
	t.Helper()
	n, err := parseReader(strings.NewReader(doc), ctx, baseDir)
	if err != nil {
		t.Fatalf("parseReader(%q): %v", doc, err)
	}
	return n
}

// parseErr asserts that a document fails to parse (covers an error branch).
func parseErr(t *testing.T, doc string) {
	t.Helper()
	if _, err := parseReader(strings.NewReader(doc), newCtx(), "."); err == nil {
		t.Fatalf("parseReader(%q): expected error", doc)
	}
}

func TestFindRootErrors(t *testing.T) {
	parseErr(t, "")                   // empty document: EOF before any element
	parseErr(t, "<Root></Root>")      // wrong root element
	parseErr(t, "<Menu>")             // decodeMenu hits EOF with no </Menu>
	parseErr(t, "<Menu><Name>x")      // text() hits EOF mid-value
	parseErr(t, "<Menu><Directory>d") // Directory text() EOF
	parseErr(t, "<Menu><AppDir>d")    // appendPath text() EOF
}

func TestRuleParseErrors(t *testing.T) {
	parseErr(t, "<Menu><Include>")            // decodeSubRules EOF
	parseErr(t, "<Menu><Include><And>")       // nested And decodeSubRules EOF
	parseErr(t, "<Menu><Include><Or>")        // Or EOF
	parseErr(t, "<Menu><Include><Not>")       // Not EOF
	parseErr(t, "<Menu><Include><Category>c") // Category text EOF
	parseErr(t, "<Menu><Include><Filename>f") // Filename text EOF
	parseErr(t, "<Menu><Exclude><Category>c") // Exclude clause error path
}

func TestMoveParseErrors(t *testing.T) {
	parseErr(t, "<Menu><Move>")        // decodeMove Token EOF
	parseErr(t, "<Menu><Move><Old>o")  // Old text EOF
	parseErr(t, "<Menu><Move><New>n")  // New text EOF
	parseErr(t, "<Menu><Move><Weird>") // unknown child Skip EOF
}

func TestNestedAndMergeSkipErrors(t *testing.T) {
	parseErr(t, "<Menu><Name>R</Name><Menu>") // nested Menu decode error
	parseErr(t, "<Menu><DefaultMergeDirs>")   // DefaultMergeDirs Skip EOF
}

func TestLayoutParseErrors(t *testing.T) {
	parseErr(t, "<Menu><Layout>")            // decodeLayout Token EOF
	parseErr(t, "<Menu><Layout><Menuname>m") // Menuname text EOF
	parseErr(t, "<Menu><Layout><Filename>f") // Filename text EOF
	parseErr(t, "<Menu><DefaultLayout>")     // DefaultLayout EOF
}

// TestHandleStartAllElements exercises every element branch of handleStart,
// including the toggles, explicit dirs, unknown elements, Move with an
// unknown child, and the layout item kinds.
func TestHandleStartAllElements(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "apps")
	doc := `<Menu>
	  <Name>Root</Name>
	  <AppDir>` + abs + `</AppDir>
	  <AppDir>rel/apps</AppDir>
	  <DirectoryDir>rel/dirs</DirectoryDir>
	  <OnlyUnallocated/>
	  <NotOnlyUnallocated/>
	  <Deleted/>
	  <NotDeleted/>
	  <MergeDir>no-such-merge-dir</MergeDir>
	  <Unknown><Nested/></Unknown>
	  <Include><All/><Bogus/></Include>
	  <Move><Old>a</Old><New>b</New><Weird/></Move>
	  <Layout>
	    <Menuname>Sub</Menuname>
	    <Filename>x.desktop</Filename>
	    <Separator/>
	    <Merge type="all"/>
	    <Junk/>
	  </Layout>
	</Menu>`
	n := parse(t, doc, newCtx(), "/base")

	if len(n.appDirs) != 2 || n.appDirs[0] != abs || n.appDirs[1] != "/base/rel/apps" {
		t.Errorf("appDirs = %v", n.appDirs)
	}
	if len(n.dirDirs) != 1 || n.dirDirs[0] != "/base/rel/dirs" {
		t.Errorf("dirDirs = %v", n.dirDirs)
	}
	if n.onlyUnalloc { // last toggle was NotOnlyUnallocated
		t.Error("onlyUnalloc should be false after NotOnlyUnallocated")
	}
	if n.deleted { // last toggle was NotDeleted
		t.Error("deleted should be false after NotDeleted")
	}
	if len(n.moves) != 1 || n.moves[0] != (moveOp{old: "a", new: "b"}) {
		t.Errorf("moves = %v", n.moves)
	}
	if n.layout == nil || len(n.layout.items) != 4 {
		t.Fatalf("layout items = %+v", n.layout)
	}
}

func TestResolvePath(t *testing.T) {
	if got := resolvePath("/base", "/abs/path"); got != "/abs/path" {
		t.Errorf("absolute resolvePath = %q", got)
	}
	if got := resolvePath("/base", "rel"); got != "/base/rel" {
		t.Errorf("relative resolvePath = %q", got)
	}
}

func TestMergeFileTypes(t *testing.T) {
	dir := t.TempDir()
	// A valid parent menu that contributes a submenu.
	parent := filepath.Join(dir, "parent.menu")
	writeFile(t, parent, `<Menu><Name>P</Name><Menu><Name>FromParent</Name></Menu></Menu>`)

	ctx := newCtx()
	ctx.parentFiles = []string{filepath.Join(dir, "absent.menu"), parent}
	n := parse(t, `<Menu><Name>R</Name><MergeFile type="parent">ignored</MergeFile></Menu>`, ctx, dir)
	if childByName(n, "FromParent") == nil {
		t.Error("parent merge did not contribute FromParent")
	}

	// type="parent" with no existing candidate: a silent no-op.
	ctx2 := newCtx()
	ctx2.parentFiles = []string{filepath.Join(dir, "none.menu")}
	n2 := parse(t, `<Menu><Name>R</Name><MergeFile type="parent"/></Menu>`, ctx2, dir)
	if len(n2.children) != 0 {
		t.Errorf("expected no children, got %d", len(n2.children))
	}

	// MergeFile text() error branch.
	if _, err := parseReader(strings.NewReader(`<Menu><MergeFile>x`), newCtx(), dir); err == nil {
		t.Error("expected MergeFile text EOF error")
	}
}

func TestMergePathBehaviors(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.menu")
	writeFile(t, good, `<Menu><Name>G</Name><Menu><Name>Added</Name></Menu></Menu>`)
	bad := filepath.Join(dir, "bad.menu")
	writeFile(t, bad, `<Menu><Name>`) // truncated -> parse error

	// Missing merge file: ignored.
	parse(t, `<Menu><Name>R</Name><MergeFile>missing.menu</MergeFile></Menu>`, newCtx(), dir)

	// Cycle guard: the same file merged twice only contributes once.
	ctx := newCtx()
	ctx.visited[mustAbs(t, good)] = true
	n := parse(t, `<Menu><Name>R</Name><MergeFile>good.menu</MergeFile></Menu>`, ctx, dir)
	if childByName(n, "Added") != nil {
		t.Error("visited file should have been skipped")
	}

	// Malformed merge file: propagated as an error.
	if _, err := parseReader(strings.NewReader(`<Menu><Name>R</Name><MergeFile>bad.menu</MergeFile></Menu>`), newCtx(), dir); err == nil {
		t.Error("expected error from malformed merge file")
	}
}

func TestMergeDirBehaviors(t *testing.T) {
	dir := t.TempDir()
	mdir := filepath.Join(dir, "merged")
	if err := os.Mkdir(mdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(mdir, "a.menu"), `<Menu><Name>M</Name><Menu><Name>DirMerged</Name></Menu></Menu>`)
	writeFile(t, filepath.Join(mdir, "skip.txt"), `not a menu`)

	n := parse(t, `<Menu><Name>R</Name><MergeDir>merged</MergeDir></Menu>`, newCtx(), dir)
	if childByName(n, "DirMerged") == nil {
		t.Error("MergeDir did not contribute DirMerged")
	}

	// MergeDir text() error branch.
	if _, err := parseReader(strings.NewReader(`<Menu><MergeDir>x`), newCtx(), dir); err == nil {
		t.Error("expected MergeDir text EOF error")
	}

	// A malformed .menu inside a merge dir surfaces as an error.
	writeFile(t, filepath.Join(mdir, "b.menu"), `<Menu><Name>`)
	if _, err := parseReader(strings.NewReader(`<Menu><Name>R</Name><MergeDir>merged</MergeDir></Menu>`), newCtx(), dir); err == nil {
		t.Error("expected error from malformed file in merge dir")
	}
}

func TestDefaultMergeDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "x.menu"), `<Menu><Name>M</Name><Menu><Name>DefMerged</Name></Menu></Menu>`)
	ctx := newCtx()
	ctx.mergeDirs = []string{dir, filepath.Join(dir, "nope")}
	n := parse(t, `<Menu><Name>R</Name><DefaultMergeDirs/></Menu>`, ctx, dir)
	if childByName(n, "DefMerged") == nil {
		t.Error("DefaultMergeDirs did not contribute DefMerged")
	}

	// A malformed .menu reached through DefaultMergeDirs surfaces as an error.
	badDir := t.TempDir()
	writeFile(t, filepath.Join(badDir, "bad.menu"), `<Menu><Name>`)
	ctx2 := newCtx()
	ctx2.mergeDirs = []string{badDir}
	if _, err := parseReader(strings.NewReader(`<Menu><Name>R</Name><DefaultMergeDirs/></Menu>`), ctx2, dir); err == nil {
		t.Error("expected error from malformed file via DefaultMergeDirs")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}
