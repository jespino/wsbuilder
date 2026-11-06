// Package site turns a workshop directory into a static website.
package site

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jespino/workshops-framework/internal/config"
	"github.com/jespino/workshops-framework/internal/markdown"
	"github.com/jespino/workshops-framework/internal/theme"
)

// Options selects what a build does.
type Options struct {
	Root   string   // workshop directory
	Output string   // overrides paths.output
	Only   []string // modules to include, ignoring draft status
	Skip   []string // modules to exclude
	Drafts bool     // include draft modules
	DryRun bool     // validate only, writing nothing
}

// Diag is a problem found during a build.
type Diag struct {
	Error bool
	File  string
	Line  int
	Msg   string
}

func (d Diag) String() string {
	loc := d.File
	if d.Line > 0 {
		loc = fmt.Sprintf("%s:%d", d.File, d.Line)
	}
	if loc == "" {
		return d.Msg
	}
	return loc + ": " + d.Msg
}

// Report describes a finished build.
type Report struct {
	Output    string
	Modules   int
	Languages int
	Pages     int
	Diags     []Diag
}

// Errors counts the diagnostics that make a build fail.
func (r *Report) Errors() int {
	n := 0
	for _, d := range r.Diags {
		if d.Error {
			n++
		}
	}
	return n
}

// ErrInvalid is returned when a workshop has problems that fail the build. The
// details are in the report's diagnostics.
var ErrInvalid = errors.New("the workshop has errors")

type builder struct {
	root   string
	opts   Options
	cfg    *config.Config
	report *Report
	themeF *theme.Theme

	assetHash map[string]string
	templates map[string]*template.Template
}

// Build generates the site. It validates everything first and replaces the
// output directory only once a complete new version exists, so a failed build
// leaves the previous one untouched.
func Build(opts Options) (*Report, error) {
	report := &Report{}
	cfg, err := config.Load(opts.Root)
	if err != nil {
		return report, err
	}

	output := cfg.Paths.Output
	if opts.Output != "" {
		output = opts.Output
		if err := cfg.ValidateOutput(opts.Root, output); err != nil {
			return report, err
		}
	}
	report.Output = filepath.Join(opts.Root, filepath.FromSlash(output))

	selected, err := theme.New(cfg.Theme.Name, filepath.Join(opts.Root, filepath.FromSlash(cfg.Paths.Theme)))
	if err != nil {
		return report, fmt.Errorf("%s: %w", config.FileName, err)
	}

	b := &builder{
		root:      opts.Root,
		opts:      opts,
		cfg:       cfg,
		report:    report,
		themeF:    selected,
		assetHash: map[string]string{},
		templates: map[string]*template.Template{},
	}

	if err := b.checkTheme(); err != nil {
		return report, err
	}

	loaded, err := b.loadContent()
	if err != nil {
		return report, err
	}
	modules, err := b.selectModules(loaded.Modules)
	if err != nil {
		return report, err
	}
	if len(modules) == 0 && report.Errors() == 0 {
		return report, fmt.Errorf("no modules to build in %s/", cfg.Paths.Modules)
	}

	rendered, err := b.renderContent(loaded, modules)
	if err != nil {
		return report, err
	}
	if report.Errors() > 0 {
		return report, ErrInvalid
	}

	report.Modules = len(modules)
	report.Languages = len(cfg.Languages)
	if opts.DryRun {
		return report, nil
	}

	tmp := report.Output + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return report, err
	}
	defer os.RemoveAll(tmp)

	if err := b.writeSite(tmp, rendered); err != nil {
		return report, err
	}
	return report, swapDir(tmp, report.Output)
}

// swapDir replaces dst with src.
func swapDir(src, dst string) error {
	old := dst + ".old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if err := os.Rename(dst, old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(old)
}

// Clean removes the output directory.
func Clean(root string) (string, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, filepath.FromSlash(cfg.Paths.Output))
	return dir, os.RemoveAll(dir)
}

func (b *builder) add(d Diag) { b.report.Diags = append(b.report.Diags, d) }

func (b *builder) errorf(file string, line int, format string, args ...any) {
	b.add(Diag{Error: true, File: file, Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (b *builder) warnf(file string, line int, format string, args ...any) {
	b.add(Diag{File: file, Line: line, Msg: fmt.Sprintf(format, args...)})
}

// lineOfText finds where a link or image appears, for a useful message.
func lineOfText(src *source, needle string) int {
	i := bytes.Index(src.Raw, []byte("("+needle))
	if i < 0 {
		return 0
	}
	return bytes.Count(src.Raw[:i], []byte("\n")) + 1
}

func (b *builder) errorfAt(src *source, needle, format string, args ...any) {
	b.errorf(src.Path, lineOfText(src, needle), format, args...)
}

func (b *builder) warnfAt(src *source, needle, format string, args ...any) {
	b.warnf(src.Path, lineOfText(src, needle), format, args...)
}

// uiFor merges the interface strings for a language: English, then the built-in
// translation, then the overrides in workshop.yaml.
func uiFor(lang config.Language) map[string]string {
	ui := maps.Clone(builtinUI["en"])
	maps.Copy(ui, builtinUI[lang.Code])
	maps.Copy(ui, lang.UI)
	return ui
}

func (b *builder) markdownTracks() []markdown.TrackInfo {
	tracks := make([]markdown.TrackInfo, len(b.cfg.Tracks))
	for i, t := range b.cfg.Tracks {
		tracks[i] = markdown.TrackInfo{ID: t.ID, Name: t.Name}
	}
	return tracks
}

// renderedModule is a module in one language, ready for its template.
type renderedModule struct {
	mod         *module
	lang        config.Language
	src         *source
	result      *markdown.Result
	title       string
	description string
	part        string
	duration    string
	draft       bool
	fallback    bool // shown in the default language: no translation exists
	exercise    *exercise
}

// renderedSite is the rendered content of all languages.
type renderedSite struct {
	modules map[string][]*renderedModule // by language code
	home    map[string]*markdown.Result
	homeSrc map[string]*source
}

func (b *builder) render(src *source, lang config.Language, resolver *linkResolver) (*markdown.Result, error) {
	result, err := markdown.Render(src.Body, markdown.Options{
		Root:       b.root,
		Tracks:     b.markdownTracks(),
		Labels:     uiFor(lang),
		Link:       resolver.link,
		Image:      resolver.image,
		LineOffset: src.Lines,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src.Path, err)
	}
	for _, p := range result.Problems {
		b.errorf(src.Path, p.Line, "%s", p.Msg)
	}
	return result, nil
}

// renderContent renders every selected module and landing page in every language.
func (b *builder) renderContent(c *content, selected []*module) (*renderedSite, error) {
	selectedSet := map[string]bool{}
	for _, mod := range selected {
		selectedSet[mod.Slug] = true
	}
	known := map[string]bool{}
	for _, mod := range c.Modules {
		known[mod.Slug] = true
	}

	site := &renderedSite{
		modules: map[string][]*renderedModule{},
		home:    map[string]*markdown.Result{},
		homeSrc: map[string]*source{},
	}
	defaultLang := b.cfg.DefaultLanguage()

	exercises := map[string]*exercise{}
	for _, mod := range selected {
		src := mod.Sources[defaultLang.Code]
		ex, err := b.findExercise(mod, src)
		if err != nil {
			b.errorf(src.Path, 0, "%v", err)
			continue
		}
		exercises[mod.Slug] = ex
	}

	for i, lang := range b.cfg.Languages {
		prefix := ""
		if i > 0 {
			prefix = "../"
		}
		for _, mod := range selected {
			base := mod.Sources[defaultLang.Code]
			src, translated := mod.Sources[lang.Code]
			if !translated {
				src = base
				b.warnf(base.Path, 0, "no %s translation: readers will see the %s text", lang.Code, defaultLang.Code)
			}

			result, err := b.renderOnce(src, lang, &linkResolver{
				b: b, file: src, selected: selectedSet, known: known, prefix: prefix,
			}, !translated)
			if err != nil {
				return nil, err
			}

			rm := &renderedModule{
				mod: mod, lang: lang, src: src, result: result,
				fallback: !translated,
				draft:    base.Meta.Draft,
				exercise: exercises[mod.Slug],
			}
			rm.title = first(src.Meta.Title, result.Title, base.Meta.Title, humanize(mod.Slug))
			rm.description = first(src.Meta.Description, base.Meta.Description, result.Summary)
			rm.part = first(src.Meta.Part, base.Meta.Part)
			rm.duration = first(src.Meta.Duration, base.Meta.Duration)
			site.modules[lang.Code] = append(site.modules[lang.Code], rm)
		}

		src := c.Home[lang.Code]
		if src == nil {
			src = c.Home[defaultLang.Code]
		}
		if src != nil {
			result, err := b.renderOnce(src, lang, &linkResolver{
				b: b, file: src, selected: selectedSet, known: known, prefix: prefix,
			}, c.Home[lang.Code] == nil)
			if err != nil {
				return nil, err
			}
			site.home[lang.Code] = result
			site.homeSrc[lang.Code] = src
		}
	}

	if len(b.cfg.Tracks) > 0 {
		b.checkTrackParity(site)
	}
	if b.cfg.URL == "" {
		b.warnf(config.FileName, 0, "`url` is not set: pages get no canonical URLs and no sitemap is generated")
	}
	if b.cfg.Author.Avatar != "" {
		if _, ok := b.imagePath(b.cfg.Author.Avatar); !ok {
			b.errorf(config.FileName, 0, "author.avatar %q not found in %s/", b.cfg.Author.Avatar, b.cfg.Paths.Images)
		}
	}
	if b.cfg.Image != "" {
		if _, ok := b.imagePath(b.cfg.Image); !ok {
			b.errorf(config.FileName, 0, "image %q not found in %s/", b.cfg.Image, b.cfg.Paths.Images)
		}
	}
	return site, nil
}

// renderOnce renders a source, discarding diagnostics on repeat renders of the
// same file for another language.
func (b *builder) renderOnce(src *source, lang config.Language, r *linkResolver, repeat bool) (*markdown.Result, error) {
	mark := len(b.report.Diags)
	result, err := b.render(src, lang, r)
	if repeat {
		b.report.Diags = b.report.Diags[:mark]
	}
	return result, err
}

// checkTrackParity warns about modules where only some tracks got content, the
// classic way to leave part of the room without an exercise.
func (b *builder) checkTrackParity(site *renderedSite) {
	for _, modules := range site.modules {
		for _, rm := range modules {
			if rm.fallback || len(rm.result.Tracks) == 0 {
				continue
			}
			have := map[string]bool{}
			for _, id := range rm.result.Tracks {
				have[id] = true
			}
			for _, track := range b.cfg.Tracks {
				if !have[track.ID] {
					b.warnf(rm.src.Path, 0, "has track blocks but none for %q", track.ID)
				}
			}
		}
	}
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// writeSite writes the whole site to dir.
func (b *builder) writeSite(dir string, site *renderedSite) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	for _, name := range theme.Assets {
		data, err := b.themeF.Read(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		b.assetHash[name] = hex.EncodeToString(sum[:4])
	}
	for _, name := range []string{theme.ThemeCSS, theme.CustomCSS} {
		data, ok := b.themeF.Lookup(name)
		if !ok {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		b.assetHash[name] = hex.EncodeToString(sum[:4])
	}

	if err := copyTree(filepath.Join(b.root, filepath.FromSlash(b.cfg.Paths.Images)), filepath.Join(dir, "images")); err != nil {
		return fmt.Errorf("copying images: %w", err)
	}
	if err := b.writeExercises(dir, site); err != nil {
		return fmt.Errorf("packing exercises: %w", err)
	}
	if err := b.writePages(dir, site); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "robots.txt"), b.renderRobots(), 0o644); err != nil {
		return err
	}
	// GitHub Pages would otherwise run the site through Jekyll.
	if err := os.WriteFile(filepath.Join(dir, ".nojekyll"), nil, 0o644); err != nil {
		return err
	}
	// Last, so that a workshop can replace anything above, like robots.txt.
	return copyTree(filepath.Join(b.root, filepath.FromSlash(b.cfg.Paths.Public)), dir)
}

func (b *builder) writeExercises(dir string, site *renderedSite) error {
	for _, rm := range site.modules[b.cfg.DefaultLanguage().Code] {
		ex := rm.exercise
		if ex == nil {
			continue
		}
		src := filepath.Join(b.root, filepath.FromSlash(ex.Dir))
		slug := rm.mod.Slug
		if len(ex.Files) > 0 {
			if err := writeZip(filepath.Join(dir, "downloads", slug+".zip"), src, slug, ex.Files); err != nil {
				return err
			}
		}
		if len(ex.Solutions) > 0 {
			if err := writeZip(filepath.Join(dir, "downloads", slug+"-solution.zip"), src, slug, ex.Solutions); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyTree mirrors a directory. A missing source is fine: images and public
// files are optional.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".git") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// repoTree links to a directory in the repository.
func (b *builder) repoTree(rel string) string {
	if b.cfg.Repository == "" {
		return ""
	}
	return b.cfg.Repository + "/tree/" + b.cfg.Branch + "/" + path.Clean(rel)
}
