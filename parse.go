// Copyright (c) the go-freedesktop/menu authors
//
// SPDX-License-Identifier: BSD-3-Clause

package menu

import (
	"encoding/xml"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// node is the raw, still-unresolved form of a <Menu> element: the parser
// produces a node tree with <MergeFile>/<MergeDir>/<DefaultMergeDirs>
// already inlined, ready for consolidation and allocation.
type node struct {
	name        string
	directories []string // <Directory> values, in document order
	appDirs     []string // resolved <AppDir> / <DefaultAppDirs> directories
	dirDirs     []string // resolved <DirectoryDir> / <DefaultDirectoryDirs>
	clauses     []clause // ordered <Include>/<Exclude> rule sets
	children    []*node  // nested <Menu> elements
	moves       []moveOp // <Move> operations
	onlyUnalloc bool     // last <OnlyUnallocated>/<NotOnlyUnallocated> wins
	deleted     bool     // last <Deleted>/<NotDeleted> wins
	layout      *layout  // <Layout>, or nil
	defLayout   *layout  // <DefaultLayout>, or nil
}

// clause is a single <Include> (include=true) or <Exclude> rule set.
type clause struct {
	include bool
	rule    rule
}

// moveOp is one <Old>/<New> pair of a <Move>.
type moveOp struct{ old, new string }

// parseCtx carries the directory expansions and the cycle guard across a
// parse (including recursive merges).
type parseCtx struct {
	appDirs     []string // <DefaultAppDirs> expansion
	dirDirs     []string // <DefaultDirectoryDirs> expansion
	mergeDirs   []string // <DefaultMergeDirs> expansion
	parentFiles []string // <MergeFile type="parent"> candidates, best first
	visited     map[string]bool
}

// parser wraps an XML decoder together with the parse context.
type parser struct {
	dec *xml.Decoder
	ctx *parseCtx
}

// parseMenuFile reads and parses a .menu file at path into a resolved node
// tree (merges inlined). A missing or malformed file is an error.
func parseMenuFile(path string, ctx *parseCtx) (*node, error) {
	f, err := os.Open(filepath.FromSlash(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Host path in, document path out: everything below resolvePath works in
	// slashes.
	return parseReader(f, ctx, filepath.ToSlash(filepath.Dir(path)))
}

// parseReader parses a menu document from r; baseDir resolves the relative
// <AppDir>/<DirectoryDir>/<MergeFile>/<MergeDir> paths it contains.
func parseReader(r io.Reader, ctx *parseCtx, baseDir string) (*node, error) {
	p := &parser{dec: xml.NewDecoder(r), ctx: ctx}
	if err := p.findRoot(); err != nil {
		return nil, err
	}
	return p.decodeMenu(baseDir)
}

// findRoot advances to the top-level <Menu> start element, skipping the XML
// declaration, DOCTYPE and whitespace. It errors if the document ends first.
func (p *parser) findRoot() error {
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return err
		}
		if se, ok := tok.(xml.StartElement); ok {
			if se.Name.Local != "Menu" {
				return &xml.SyntaxError{Msg: "menu: root element is <" + se.Name.Local + ">, want <Menu>"}
			}
			return nil
		}
	}
}

// decodeMenu consumes tokens up to the matching </Menu>, filling a node.
func (p *parser) decodeMenu(baseDir string) (*node, error) {
	n := &node{}
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := p.handleStart(n, t, baseDir); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if t.Name.Local == "Menu" {
				return n, nil
			}
		}
	}
}

// handleStart dispatches a child element of a <Menu>.
func (p *parser) handleStart(n *node, se xml.StartElement, baseDir string) error {
	switch se.Name.Local {
	case "Name":
		s, err := p.text()
		if err != nil {
			return err
		}
		n.name = s
	case "Directory":
		s, err := p.text()
		if err != nil {
			return err
		}
		n.directories = append(n.directories, s)
	case "AppDir":
		return p.appendPath(&n.appDirs, baseDir)
	case "DirectoryDir":
		return p.appendPath(&n.dirDirs, baseDir)
	case "DefaultAppDirs":
		n.appDirs = append(n.appDirs, p.ctx.appDirs...)
		return p.dec.Skip()
	case "DefaultDirectoryDirs":
		n.dirDirs = append(n.dirDirs, p.ctx.dirDirs...)
		return p.dec.Skip()
	case "DefaultMergeDirs":
		if err := p.dec.Skip(); err != nil {
			return err
		}
		for _, d := range p.ctx.mergeDirs {
			if err := p.mergeDir(n, d); err != nil {
				return err
			}
		}
	case "OnlyUnallocated":
		n.onlyUnalloc = true
		return p.dec.Skip()
	case "NotOnlyUnallocated":
		n.onlyUnalloc = false
		return p.dec.Skip()
	case "Deleted":
		n.deleted = true
		return p.dec.Skip()
	case "NotDeleted":
		n.deleted = false
		return p.dec.Skip()
	case "MergeFile":
		return p.handleMergeFile(n, se, baseDir)
	case "MergeDir":
		s, err := p.text()
		if err != nil {
			return err
		}
		return p.mergeDir(n, resolvePath(baseDir, s))
	case "Include", "Exclude":
		r, err := p.decodeRuleSet(se.Name.Local)
		if err != nil {
			return err
		}
		n.clauses = append(n.clauses, clause{include: se.Name.Local == "Include", rule: r})
	case "Menu":
		child, err := p.decodeMenu(baseDir)
		if err != nil {
			return err
		}
		n.children = append(n.children, child)
	case "Move":
		return p.decodeMove(n)
	case "Layout":
		lay, err := p.decodeLayout("Layout")
		if err != nil {
			return err
		}
		n.layout = lay
	case "DefaultLayout":
		lay, err := p.decodeLayout("DefaultLayout")
		if err != nil {
			return err
		}
		n.defLayout = lay
	default:
		return p.dec.Skip()
	}
	return nil
}

// appendPath reads the current element's text as a path, resolves it against
// baseDir and appends it to dst.
func (p *parser) appendPath(dst *[]string, baseDir string) error {
	s, err := p.text()
	if err != nil {
		return err
	}
	*dst = append(*dst, resolvePath(baseDir, s))
	return nil
}

// handleMergeFile processes a <MergeFile>, honoring type="path" (default,
// merge the referenced file) and type="parent" (merge the next
// applications.menu found in the context's parent candidates).
func (p *parser) handleMergeFile(n *node, se xml.StartElement, baseDir string) error {
	typ := "path"
	for _, a := range se.Attr {
		if a.Name.Local == "type" {
			typ = a.Value
		}
	}
	s, err := p.text()
	if err != nil {
		return err
	}
	if typ == "parent" {
		for _, cand := range p.ctx.parentFiles {
			if fileExists(cand) {
				return p.mergePath(n, cand)
			}
		}
		return nil
	}
	return p.mergePath(n, resolvePath(baseDir, s))
}

// mergeDir merges every *.menu file directly under dir, in sorted order.
func (p *parser) mergeDir(n *node, dir string) error {
	entries, err := os.ReadDir(filepath.FromSlash(dir))
	if err != nil {
		return nil // a missing merge directory is ignored, per spec
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".menu") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := p.mergePath(n, path.Join(dir, name)); err != nil {
			return err
		}
	}
	return nil
}

// mergePath parses the menu file at path and inlines its root menu's content
// into n. A missing file is ignored; a malformed one is an error; a file
// already merged on this path (cycle) is skipped.
func (p *parser) mergePath(n *node, path string) error {
	// ToSlash so the cycle key is the same string on every platform; two
	// spellings of one file would merge it twice.
	absHost, _ := filepath.Abs(filepath.FromSlash(path))
	abs := filepath.ToSlash(absHost)
	if p.ctx.visited[abs] {
		return nil
	}
	if !fileExists(path) {
		return nil
	}
	p.ctx.visited[abs] = true
	merged, err := parseMenuFile(path, p.ctx)
	if err != nil {
		return err
	}
	mergeContent(n, merged)
	return nil
}

// text accumulates the character data of the current element up to its end
// tag and returns it trimmed.
func (p *parser) text() (string, error) {
	var b strings.Builder
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			b.Write(t)
		case xml.EndElement:
			return strings.TrimSpace(b.String()), nil
		}
	}
}

// decodeRuleSet reads the children of an <Include>/<Exclude> up to its end
// tag and returns them as a single OR rule (an entry matches the set if it
// matches any contained rule).
func (p *parser) decodeRuleSet(elem string) (rule, error) {
	subs, err := p.decodeSubRules(elem)
	if err != nil {
		return rule{}, err
	}
	return rule{op: opOr, subs: subs}, nil
}

// decodeSubRules reads rule elements until the </elem> tag.
func (p *parser) decodeSubRules(elem string) ([]rule, error) {
	var subs []rule
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			r, err := p.decodeRule(t)
			if err != nil {
				return nil, err
			}
			subs = append(subs, r)
		case xml.EndElement:
			if t.Name.Local == elem {
				return subs, nil
			}
		}
	}
}

// decodeRule decodes a single match rule element.
func (p *parser) decodeRule(se xml.StartElement) (rule, error) {
	switch se.Name.Local {
	case "All":
		return rule{op: opAll}, p.dec.Skip()
	case "Category":
		s, err := p.text()
		return rule{op: opCategory, value: s}, err
	case "Filename":
		s, err := p.text()
		return rule{op: opFilename, value: s}, err
	case "And":
		subs, err := p.decodeSubRules("And")
		return rule{op: opAnd, subs: subs}, err
	case "Or":
		subs, err := p.decodeSubRules("Or")
		return rule{op: opOr, subs: subs}, err
	case "Not":
		subs, err := p.decodeSubRules("Not")
		return rule{op: opNot, subs: subs}, err
	default:
		return rule{op: opNone}, p.dec.Skip()
	}
}

// decodeMove reads a <Move> element's <Old>/<New> pairs.
func (p *parser) decodeMove(n *node) error {
	var old string
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "Old":
				if old, err = p.text(); err != nil {
					return err
				}
			case "New":
				nw, err := p.text()
				if err != nil {
					return err
				}
				n.moves = append(n.moves, moveOp{old: old, new: nw})
			default:
				if err := p.dec.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if t.Name.Local == "Move" {
				return nil
			}
		}
	}
}

// decodeLayout reads a <Layout>/<DefaultLayout> element into a layout.
func (p *parser) decodeLayout(elem string) (*layout, error) {
	lay := &layout{}
	for {
		tok, err := p.dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := p.layoutItem(lay, t); err != nil {
				return nil, err
			}
		case xml.EndElement:
			if t.Name.Local == elem {
				return lay, nil
			}
		}
	}
}

// layoutItem decodes one child of a layout element.
func (p *parser) layoutItem(lay *layout, se xml.StartElement) error {
	switch se.Name.Local {
	case "Menuname":
		s, err := p.text()
		lay.items = append(lay.items, layoutEntry{kind: layMenuname, value: s})
		return err
	case "Filename":
		s, err := p.text()
		lay.items = append(lay.items, layoutEntry{kind: layFilename, value: s})
		return err
	case "Separator":
		lay.items = append(lay.items, layoutEntry{kind: laySeparator})
		return p.dec.Skip()
	case "Merge":
		typ := "all"
		for _, a := range se.Attr {
			if a.Name.Local == "type" {
				typ = a.Value
			}
		}
		lay.items = append(lay.items, layoutEntry{kind: layMerge, value: typ})
		return p.dec.Skip()
	default:
		return p.dec.Skip()
	}
}

// resolvePath joins a possibly-relative spec path onto baseDir.
// ⛔ SLASHES, not filepath. The paths inside a .menu document are defined by
// the XDG menu specification, which is written in slashes; they are DOCUMENT
// paths, not host paths. Resolving them with path/filepath made
// <AppDir>rel/apps</AppDir> come out as `\base\rel\apps` on Windows -- a
// value no .menu file, no test and no other implementation would ever write.
//
// The host only enters at the three places that touch the disk, and each
// converts with filepath.FromSlash there. Windows accepts '/' in most calls
// anyway; the conversion is so the value handed to the OS is the OS's, rather
// than relying on that.
func resolvePath(baseDir, p string) string {
	if path.IsAbs(p) {
		return path.Clean(p)
	}
	return path.Join(baseDir, p)
}

// fileExists reports whether path names an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(filepath.FromSlash(path))
	return err == nil && !info.IsDir()
}
