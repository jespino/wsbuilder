// Package theme provides the website's HTML, CSS and JavaScript. Themes are
// compiled into the binary, and a file resolves in this order:
//
//  1. the workshop's theme directory, where it replaces any single file
//  2. the theme selected with theme.name in workshop.yaml
//  3. the simple theme, which is complete and is the base of all the others
//
// That lets a theme contain only what makes it different.
package theme

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

//go:embed themes
var builtin embed.FS

// The files that make up a theme.
const (
	BaseTemplate     = "base.html"
	HomeTemplate     = "home.html"
	ModuleTemplate   = "module.html"
	NotFoundTemplate = "404.html"
	Stylesheet       = "styles.css"
	Script           = "app.js"

	// ThemeCSS is loaded after the stylesheet. Themes use it to restyle the
	// base theme instead of replacing it.
	ThemeCSS = "theme.css"

	// CustomCSS is loaded last, when a workshop has one. It is the way to adjust
	// a theme without copying any of it.
	CustomCSS = "custom.css"
)

const (
	// DefaultName is the theme used when a workshop doesn't select one.
	DefaultName = "default"

	// BaseName is the theme every other theme falls back to.
	BaseName = "simple"
)

// Info describes a built-in theme.
type Info struct {
	Name        string
	Description string
}

// Available lists the built-in themes.
var Available = []Info{
	{DefaultName, "Bold cards, a dark navbar and a purple to orange gradient"},
	{BaseName, "Sidebar, table of contents and quiet module cards, for long workshops"},
	{"internals", "A single reading column, inspired by Internals for Interns"},
	{"kids", "Bright colors, rounded letters and big buttons, for young learners"},
}

// Pages lists the templates that render a page on top of the base template.
var Pages = []string{HomeTemplate, ModuleTemplate, NotFoundTemplate}

// Assets lists the files always copied to the site root.
var Assets = []string{Stylesheet, Script}

// Theme reads theme files.
type Theme struct {
	name string
	dir  string
}

// Exists reports whether name is a built-in theme.
func Exists(name string) bool {
	for _, info := range Available {
		if info.Name == name {
			return true
		}
	}
	return false
}

func names() string {
	list := make([]string, len(Available))
	for i, info := range Available {
		list[i] = info.Name
	}
	return strings.Join(list, ", ")
}

// New returns the named theme with overrides from dir, which does not need to
// exist. An empty name selects the default theme.
func New(name, dir string) (*Theme, error) {
	if name == "" {
		name = DefaultName
	}
	if !Exists(name) {
		return nil, fmt.Errorf("unknown theme %q (available: %s)", name, names())
	}
	return &Theme{name: name, dir: dir}, nil
}

// Lookup returns a theme file without falling back to the base theme. It is
// how optional files are found.
func (t *Theme) Lookup(name string) ([]byte, bool) {
	if data, err := os.ReadFile(filepath.Join(t.dir, name)); err == nil {
		return data, true
	}
	if t.name == BaseName {
		return nil, false
	}
	data, err := builtin.ReadFile("themes/" + t.name + "/" + name)
	return data, err == nil
}

// Read returns a theme file.
func (t *Theme) Read(name string) ([]byte, error) {
	if data, ok := t.Lookup(name); ok {
		return data, nil
	}
	return builtin.ReadFile("themes/" + BaseName + "/" + name)
}

// Eject writes every file of the named theme into dir, including those it
// inherits, so it can be edited. Existing files are left alone. It returns the
// names of the files it wrote.
func Eject(name, dir string) ([]string, error) {
	t, err := New(name, dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	files := map[string]bool{}
	for _, source := range []string{BaseName, t.name} {
		entries, err := fs.ReadDir(builtin, "themes/"+source)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			files[entry.Name()] = true
		}
	}

	var written []string
	for _, candidate := range sortedKeys(files) {
		target := filepath.Join(dir, candidate)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return written, err
		}
		data, err := t.Read(candidate)
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return written, fmt.Errorf("writing %s: %w", candidate, err)
		}
		written = append(written, candidate)
	}
	return written, nil
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
