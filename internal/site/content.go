package site

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/jespino/workshops-framework/internal/config"
	"gopkg.in/yaml.v3"
)

var (
	moduleFileRe = regexp.MustCompile(`^(\d+)-([^.]+)(?:\.([A-Za-z0-9-]+))?\.md$`)
	homeFileRe   = regexp.MustCompile(`^index(?:\.([A-Za-z0-9-]+))?\.md$`)
)

// frontMatter is the optional YAML block at the top of a markdown file.
type frontMatter struct {
	Title       string `yaml:"title"`
	Description string `yaml:"description"`
	Part        string `yaml:"part"`
	Duration    string `yaml:"duration"`
	Draft       bool   `yaml:"draft"`
	// Exercise names the exercise directory when it does not match the slug.
	Exercise string `yaml:"exercise"`
}

// source is one markdown file.
type source struct {
	Path  string // relative to the workshop root, for messages
	Raw   []byte // the whole file
	Body  []byte // the file without frontmatter
	Lines int    // lines of frontmatter, to report correct line numbers
	Meta  frontMatter
}

// module is one module in all its languages.
type module struct {
	Slug    string // file name without language or extension: "03-preallocation"
	Number  int
	Sources map[string]*source // by language code
}

// content is everything found in the modules directory.
type content struct {
	Modules []*module
	Home    map[string]*source // the optional landing page text, by language
}

// loadContent reads the modules directory. Files it cannot place are reported
// instead of ignored, because a skipped file is a module silently missing.
func (b *builder) loadContent() (*content, error) {
	dir := filepath.Join(b.root, filepath.FromSlash(b.cfg.Paths.Modules))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("modules directory %q not found", b.cfg.Paths.Modules)
		}
		return nil, err
	}

	defaultLang := b.cfg.DefaultLanguage().Code
	c := &content{Home: map[string]*source{}}
	bySlug := map[string]*module{}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.EqualFold(name, "README.md") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(b.cfg.Paths.Modules, name))

		var lang string
		match := moduleFileRe.FindStringSubmatch(name)
		home := homeFileRe.FindStringSubmatch(name)
		switch {
		case match != nil:
			lang = match[3]
		case home != nil:
			lang = home[1]
		default:
			b.warnf(rel, 0, "skipped: module files are named NN-name.md")
			continue
		}
		if lang == "" {
			lang = defaultLang
		} else if lang == defaultLang {
			b.warnf(rel, 0, "skipped: %q is the default language, so this file should have no language suffix", lang)
			continue
		} else if _, ok := b.cfg.Language(lang); !ok {
			b.warnf(rel, 0, "skipped: language %q is not listed in %s", lang, config.FileName)
			continue
		}

		src, err := readSource(filepath.Join(dir, name), rel)
		if err != nil {
			b.errorf(rel, 0, "%v", err)
			continue
		}

		if home != nil {
			c.Home[lang] = src
			continue
		}

		slug := match[1] + "-" + match[2]
		mod := bySlug[slug]
		if mod == nil {
			number, err := strconv.Atoi(match[1])
			if err != nil {
				b.errorf(rel, 0, "invalid module number %q", match[1])
				continue
			}
			mod = &module{Slug: slug, Number: number, Sources: map[string]*source{}}
			bySlug[slug] = mod
			c.Modules = append(c.Modules, mod)
		}
		mod.Sources[lang] = src
	}

	sort.SliceStable(c.Modules, func(i, j int) bool {
		if c.Modules[i].Number != c.Modules[j].Number {
			return c.Modules[i].Number < c.Modules[j].Number
		}
		return c.Modules[i].Slug < c.Modules[j].Slug
	})

	for i, mod := range c.Modules {
		if mod.Sources[defaultLang] == nil {
			b.errorf(b.cfg.Paths.Modules, 0, "module %s has no %s version (%s.md)", mod.Slug, defaultLang, mod.Slug)
		}
		if i > 0 && c.Modules[i-1].Number == mod.Number {
			b.warnf(b.cfg.Paths.Modules, 0, "modules %s and %s share the number %d", c.Modules[i-1].Slug, mod.Slug, mod.Number)
		}
	}
	return c, nil
}

func readSource(path, rel string) (*source, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src := &source{Path: rel, Raw: raw}
	front, body, lines, err := splitFrontMatter(raw)
	if err != nil {
		return nil, err
	}
	src.Body, src.Lines = body, lines
	if front != nil {
		dec := yaml.NewDecoder(bytes.NewReader(front))
		dec.KnownFields(true)
		if err := dec.Decode(&src.Meta); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("frontmatter: %w", err)
		}
	}
	return src, nil
}

// splitFrontMatter separates a leading "---" block from the body. lines is the
// number of lines before the body.
func splitFrontMatter(raw []byte) (front, body []byte, lines int, err error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(normalized, []byte("---\n")) {
		return nil, normalized, 0, nil
	}
	rest := normalized[len("---\n"):]
	var end int
	switch {
	case bytes.HasPrefix(rest, []byte("---\n")):
		end = 0
	default:
		i := bytes.Index(rest, []byte("\n---\n"))
		if i < 0 {
			if bytes.HasSuffix(rest, []byte("\n---")) {
				i = len(rest) - len("\n---")
			} else {
				return nil, nil, 0, errors.New("frontmatter is never closed: expected a line with ---")
			}
		}
		end = i + 1
	}
	front = rest[:end]
	body = bytes.TrimPrefix(rest[end:], []byte("---"))
	body = bytes.TrimPrefix(body, []byte("\n"))
	return front, body, bytes.Count(normalized[:len(normalized)-len(body)], []byte("\n")), nil
}

// selectModules applies drafts, --only and --skip. Selectors are a number
// ("03") or a full slug ("03-preallocation"). An explicit --only wins over
// draft status, because naming a module is a clear enough request.
func (b *builder) selectModules(all []*module) ([]*module, error) {
	matches := func(mod *module, sel string) bool {
		return mod.Slug == sel || strings.HasPrefix(mod.Slug, sel+"-")
	}
	anyMatch := func(mod *module, sels []string) bool {
		for _, sel := range sels {
			if matches(mod, sel) {
				return true
			}
		}
		return false
	}

	for _, sel := range append(append([]string{}, b.opts.Only...), b.opts.Skip...) {
		found := false
		for _, mod := range all {
			found = found || matches(mod, sel)
		}
		if !found {
			return nil, fmt.Errorf("unknown module %q", sel)
		}
	}

	defaultLang := b.cfg.DefaultLanguage().Code
	var out []*module
	for _, mod := range all {
		src := mod.Sources[defaultLang]
		if src == nil {
			continue
		}
		include := !src.Meta.Draft || b.opts.Drafts
		if len(b.opts.Only) > 0 {
			include = anyMatch(mod, b.opts.Only)
		}
		if include && !anyMatch(mod, b.opts.Skip) {
			out = append(out, mod)
		}
	}
	return out, nil
}

// humanize turns a slug like "03-escape-analysis" into "Escape analysis".
func humanize(slug string) string {
	if _, rest, ok := strings.Cut(slug, "-"); ok {
		slug = rest
	}
	runes := []rune(strings.NewReplacer("-", " ", "_", " ").Replace(slug))
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

// slugify makes a string safe to use as an identifier.
func slugify(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}
