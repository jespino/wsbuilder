package site

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/jespino/workshops-framework/internal/config"
	"github.com/jespino/workshops-framework/internal/markdown"
	"github.com/jespino/workshops-framework/internal/theme"
)

// modulesMarker places the module list inside the landing page text.
const modulesMarker = "<!-- modules -->"

// Page is the data given to every theme template.
type Page struct {
	Lang        string
	Title       string // the content of <title>
	Description string
	UI          map[string]string
	Site        config.Localized
	Author      config.Author
	AvatarURL   string
	Repository  string
	Tracks      []config.Track
	Languages   []LanguageLink

	Parts []PartView // the modules, grouped
	Count int        // how many modules this build has
	Slugs string     // their slugs, for the progress counter

	// Base leads to the site root and Home to this language's landing page.
	Base string
	Home string

	Styles     string
	Script     string
	ThemeCSS   string
	CustomCSS  string
	InlineCSS  template.CSS
	StorageKey string
	BaseHref   string // set on the 404 page, which is served from any path

	Canonical  string
	Alternates []alternate
	OGType     string
	OGImage    string
	JSONLD     template.JS

	// Exactly one of these is set, depending on the page.
	Module  *ModuleView
	Landing *LandingView
}

// LanguageLink is one entry of the language menu.
type LanguageLink struct {
	Code    string
	Name    string
	URL     string
	Current bool
}

// PartView is a run of consecutive modules in the same part.
type PartView struct {
	Title   string
	Modules []ModuleLink
}

// ModuleLink is a module as shown in lists.
type ModuleLink struct {
	Slug        string
	URL         string
	Number      int
	Title       string
	Description string
	Duration    string
	Draft       bool
	Current     bool
}

// ModuleView is the content of a module page.
type ModuleView struct {
	ModuleLink
	Part     string
	Position int // 1-based place in this build, as numbers may have gaps
	Content  template.HTML
	Headings []markdown.Heading
	Fallback bool
	Prev     *NavLink
	Next     *NavLink
	Exercise *ExerciseView
}

// NavLink is a previous or next link.
type NavLink struct {
	URL   string
	Title string
}

// ExerciseView describes a module's downloads.
type ExerciseView struct {
	Files       []string
	ZipURL      string
	SolutionURL string
	RepoURL     string
}

// LandingView is the content of the landing page.
type LandingView struct {
	Before  template.HTML // the page text before the module list
	After   template.HTML // and after it
	StartAt string
}

func (b *builder) template(name string) (*template.Template, error) {
	if t := b.templates[name]; t != nil {
		return t, nil
	}
	funcs := template.FuncMap{
		"pad": func(n int) string { return fmt.Sprintf("%02d", n) },
	}
	base, err := b.themeF.Read(theme.BaseTemplate)
	if err != nil {
		return nil, err
	}
	t, err := template.New(theme.BaseTemplate).Funcs(funcs).Parse(string(base))
	if err != nil {
		return nil, fmt.Errorf("theme %s: %w", theme.BaseTemplate, err)
	}
	page, err := b.themeF.Read(name)
	if err != nil {
		return nil, err
	}
	if t, err = t.Parse(string(page)); err != nil {
		return nil, fmt.Errorf("theme %s: %w", name, err)
	}
	b.templates[name] = t
	return t, nil
}

// checkTheme parses every template up front, so that a broken override fails
// the build, and `check`, with a clear message.
func (b *builder) checkTheme() error {
	for _, name := range theme.Pages {
		if _, err := b.template(name); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) execute(name, out string, page *Page) error {
	t, err := b.template(name)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, page); err != nil {
		return fmt.Errorf("rendering %s: %w", out, err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	b.report.Pages++
	return os.WriteFile(out, buf.Bytes(), 0o644)
}

// langDir is where a language lives, relative to the site root.
func (b *builder) langDir(lang config.Language) string {
	if lang.Code == b.cfg.DefaultLanguage().Code {
		return ""
	}
	return lang.Code + "/"
}

func (b *builder) assetURL(base, name string) string {
	return base + name + "?v=" + b.assetHash[name]
}

func (b *builder) inlineCSS() template.CSS {
	var css strings.Builder
	var vars []string
	if c := b.cfg.Theme.Primary; c != "" {
		vars = append(vars, "--primary:"+c)
	}
	if c := b.cfg.Theme.Accent; c != "" {
		vars = append(vars, "--accent:"+c)
	}
	if len(vars) > 0 {
		css.WriteString(":root{" + strings.Join(vars, ";") + "}")
	}
	// Track ids are validated in the configuration.
	for _, t := range b.cfg.Tracks {
		fmt.Fprintf(&css, `html[data-track="%s"] .track-scoped:not([data-track="%s"]){display:none}`, t.ID, t.ID)
	}
	return template.CSS(css.String())
}

// basePage fills what every page of a language has in common.
func (b *builder) basePage(lang config.Language, modules []*renderedModule, current string) *Page {
	localized := b.cfg.Localize(lang)
	base := ""
	if b.langDir(lang) != "" {
		base = "../"
	}

	page := &Page{
		Lang:       lang.Code,
		UI:         uiFor(lang),
		Site:       localized,
		Author:     b.cfg.Author,
		Repository: b.cfg.Repository,
		Tracks:     b.cfg.Tracks,
		Base:       base,
		Home:       "index.html",
		Styles:     b.assetURL(base, theme.Stylesheet),
		Script:     b.assetURL(base, theme.Script),
		InlineCSS:  b.inlineCSS(),
		// All GitHub Pages sites of one account share an origin, and so their
		// localStorage: the key has to be specific to the workshop.
		StorageKey: "wsb:" + slugify(first(b.cfg.URL, b.cfg.Repository, b.cfg.Title)),
		OGType:     "website",
	}
	if b.assetHash[theme.ThemeCSS] != "" {
		page.ThemeCSS = b.assetURL(base, theme.ThemeCSS)
	}
	if b.assetHash[theme.CustomCSS] != "" {
		page.CustomCSS = b.assetURL(base, theme.CustomCSS)
	}
	page.Author.Bio = localized.AuthorBio
	page.Author.Role = localized.AuthorRole
	if rel, ok := b.imagePath(b.cfg.Author.Avatar); ok && b.cfg.Author.Avatar != "" {
		page.AvatarURL = base + "images/" + rel
	}
	if rel, ok := b.imagePath(b.cfg.Image); ok && b.cfg.Image != "" {
		if url := b.absURL("images/" + rel); url != "" {
			page.OGImage = url
		}
	}

	slugs := make([]string, 0, len(modules))
	for _, rm := range modules {
		slugs = append(slugs, rm.mod.Slug)
		link := ModuleLink{
			Slug:        rm.mod.Slug,
			URL:         rm.mod.Slug + ".html",
			Number:      rm.mod.Number,
			Title:       rm.title,
			Description: rm.description,
			Duration:    rm.duration,
			Draft:       rm.draft,
			Current:     rm.mod.Slug == current,
		}
		if n := len(page.Parts); n == 0 || page.Parts[n-1].Title != rm.part {
			page.Parts = append(page.Parts, PartView{Title: rm.part})
		}
		last := &page.Parts[len(page.Parts)-1]
		last.Modules = append(last.Modules, link)
	}
	page.Count = len(modules)
	page.Slugs = strings.Join(slugs, ",")
	return page
}

// languageLinks points every language at the same page. A language in which
// the page is only a fallback gets no hreflang alternate, but stays in the menu.
func (b *builder) languageLinks(from config.Language, file string, translated func(config.Language) bool) ([]LanguageLink, []alternate) {
	links := make([]LanguageLink, 0, len(b.cfg.Languages))
	var alternates []alternate
	for _, lang := range b.cfg.Languages {
		target := b.langDir(lang) + file
		links = append(links, LanguageLink{
			Code:    lang.Code,
			Name:    lang.Name,
			URL:     relativeTo(b.langDir(from), target),
			Current: lang.Code == from.Code,
		})
		if url := b.absURL(target); url != "" && translated(lang) {
			alternates = append(alternates, alternate{Lang: lang.Code, URL: url})
		}
	}
	if len(alternates) < 2 {
		return links, nil
	}
	return links, alternates
}

// relativeTo builds the link from a page in fromDir to target. Both are relative
// to the site root.
func relativeTo(fromDir, target string) string {
	if fromDir == "" {
		return target
	}
	return "../" + target
}

func (b *builder) writePages(dir string, site *renderedSite) error {
	var sitemap []sitemapEntry
	defaultLang := b.cfg.DefaultLanguage()

	for _, lang := range b.cfg.Languages {
		modules := site.modules[lang.Code]
		langDir := filepath.Join(dir, filepath.FromSlash(b.langDir(lang)))

		// Landing page.
		page := b.basePage(lang, modules, "")
		page.Title = page.Site.Title
		page.Description = page.Site.Description
		var alternates []alternate
		page.Languages, alternates = b.languageLinks(lang, "index.html", func(config.Language) bool { return true })
		page.Alternates = alternates
		page.Canonical = b.absURL(b.langDir(lang) + "index.html")
		page.JSONLD = jsonLD(b.courseData(page.Site.Title, page.Site.Description, lang.Code, page.Canonical))
		landing := &LandingView{}
		if result := site.home[lang.Code]; result != nil {
			if src := site.homeSrc[lang.Code]; src != nil {
				page.Description = first(src.Meta.Description, page.Description)
			}
			before, after, _ := strings.Cut(result.HTML, modulesMarker)
			landing.Before, landing.After = template.HTML(before), template.HTML(after)
		}
		if len(modules) > 0 {
			landing.StartAt = modules[0].mod.Slug + ".html"
		}
		page.Landing = landing
		if err := b.execute(theme.HomeTemplate, filepath.Join(langDir, "index.html"), page); err != nil {
			return err
		}
		if page.Canonical != "" {
			sitemap = append(sitemap, sitemapEntry{URL: page.Canonical, Alternates: alternates})
		}

		// Module pages.
		for i, rm := range modules {
			page, entry := b.modulePage(lang, modules, i)
			if err := b.execute(theme.ModuleTemplate, filepath.Join(langDir, rm.mod.Slug+".html"), page); err != nil {
				return err
			}
			if entry != nil {
				sitemap = append(sitemap, *entry)
			}
		}
	}

	if len(sitemap) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "sitemap.xml"), renderSitemap(sitemap), 0o644); err != nil {
			return err
		}
	}

	notFound := b.basePage(defaultLang, site.modules[defaultLang.Code], "")
	notFound.Title = notFound.UI["not_found_title"] + " — " + notFound.Site.Title
	notFound.Description = notFound.Site.Description
	notFound.Languages = nil
	// Hosts serve this page from any path, where relative links would break.
	notFound.BaseHref = b.cfg.URL
	return b.execute(theme.NotFoundTemplate, filepath.Join(dir, "404.html"), notFound)
}

func (b *builder) modulePage(lang config.Language, modules []*renderedModule, i int) (*Page, *sitemapEntry) {
	rm := modules[i]
	slug := rm.mod.Slug
	file := slug + ".html"

	page := b.basePage(lang, modules, slug)
	page.Title = rm.title + " — " + page.Site.ShortTitle
	page.Description = rm.description
	page.OGType = "article"

	var alternates []alternate
	page.Languages, alternates = b.languageLinks(lang, file, func(l config.Language) bool {
		return rm.mod.Sources[l.Code] != nil
	})
	page.Alternates = alternates

	// An untranslated page duplicates the original, so that is what search
	// engines are pointed at.
	if rm.fallback {
		page.Canonical = b.absURL(file)
	} else {
		page.Canonical = b.absURL(b.langDir(lang) + file)
	}

	view := &ModuleView{
		ModuleLink: ModuleLink{
			Slug:        slug,
			URL:         file,
			Number:      rm.mod.Number,
			Title:       rm.title,
			Description: rm.description,
			Duration:    rm.duration,
			Draft:       rm.draft,
		},
		Part:     rm.part,
		Position: i + 1,
		Content:  template.HTML(rm.result.HTML),
		Headings: rm.result.Headings,
		Fallback: rm.fallback,
	}
	if i > 0 {
		view.Prev = &NavLink{URL: modules[i-1].mod.Slug + ".html", Title: modules[i-1].title}
	} else {
		view.Prev = &NavLink{URL: "index.html", Title: page.UI["home"]}
	}
	if i < len(modules)-1 {
		view.Next = &NavLink{URL: modules[i+1].mod.Slug + ".html", Title: modules[i+1].title}
	}
	if ex := rm.exercise; ex != nil {
		ev := &ExerciseView{Files: ex.Files, RepoURL: b.repoTree(ex.Dir)}
		if len(ex.Files) > 0 {
			ev.ZipURL = page.Base + path.Join("downloads", slug+".zip")
		}
		if len(ex.Solutions) > 0 {
			ev.SolutionURL = page.Base + path.Join("downloads", slug+"-solution.zip")
		}
		view.Exercise = ev
	}
	page.Module = view

	course := b.absURL(b.langDir(lang) + "index.html")
	page.JSONLD = jsonLD(b.moduleData(rm.title, rm.description, lang.Code, page.Canonical, page.Site.Title, course, i+1))

	if rm.fallback || rm.draft || page.Canonical == "" {
		return page, nil
	}
	return page, &sitemapEntry{URL: page.Canonical, Alternates: alternates}
}
