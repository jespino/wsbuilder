// Package config loads and validates workshop.yaml, the single file that holds
// everything about a workshop that is not module content.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the configuration file that marks the root of a workshop.
const FileName = "workshop.yaml"

// Config is the content of workshop.yaml.
type Config struct {
	Title       string `yaml:"title"`
	ShortTitle  string `yaml:"short_title"`
	Description string `yaml:"description"`

	// URL is where the site is published. It enables canonical URLs, hreflang
	// alternates and the sitemap, none of which can be relative.
	URL string `yaml:"url"`

	Repository string `yaml:"repository"`
	Branch     string `yaml:"branch"`

	// Image is the social sharing image, as a path inside the images directory.
	Image string `yaml:"image"`

	Author    Author     `yaml:"author"`
	Facts     []Fact     `yaml:"facts"`
	Languages []Language `yaml:"languages"`
	Tracks    []Track    `yaml:"tracks"`
	Theme     Theme      `yaml:"theme"`
	Paths     Paths      `yaml:"paths"`

	// Exclude lists file name globs left out of exercise downloads. It is
	// matched against every path component.
	Exclude []string `yaml:"exclude"`

	// Solutions lists the entries of an exercise directory, as file name globs,
	// that hold its solution. They get a download of their own.
	Solutions []string `yaml:"solutions"`
}

// Author describes who wrote the workshop.
type Author struct {
	Name   string `yaml:"name"`
	Role   string `yaml:"role"`
	Bio    string `yaml:"bio"`
	Avatar string `yaml:"avatar"`
	Links  []Link `yaml:"links"`
}

// Link is a labelled URL.
type Link struct {
	Label string `yaml:"label"`
	URL   string `yaml:"url"`
}

// Fact is one label/value pair in the landing page hero.
type Fact struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
}

// Language is one language of the workshop. The first one listed is the
// default: it is published at the site root and its files have no language
// suffix. The optional fields translate the matching top-level settings.
type Language struct {
	Code        string            `yaml:"code"`
	Name        string            `yaml:"name"`
	Title       string            `yaml:"title"`
	ShortTitle  string            `yaml:"short_title"`
	Description string            `yaml:"description"`
	AuthorRole  string            `yaml:"author_role"`
	AuthorBio   string            `yaml:"author_bio"`
	Facts       []Fact            `yaml:"facts"`
	UI          map[string]string `yaml:"ui"`
}

// Track is one code track through the workshop, selectable by the reader.
type Track struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

// Theme selects the look of the site.
type Theme struct {
	// Name selects a built-in theme. See `wsbuilder themes`.
	Name    string `yaml:"name"`
	Primary string `yaml:"primary"`
	Accent  string `yaml:"accent"`
}

// Paths locates each part of a workshop, relative to its root.
type Paths struct {
	Modules   string `yaml:"modules"`
	Exercises string `yaml:"exercises"`
	Images    string `yaml:"images"`
	Public    string `yaml:"public"`
	Theme     string `yaml:"theme"`
	Output    string `yaml:"output"`
}

// Localized is the configuration as seen from one language.
type Localized struct {
	Title       string
	ShortTitle  string
	Description string
	AuthorRole  string
	AuthorBio   string
	Facts       []Fact
}

// DefaultExclude is what never belongs in an exercise download.
var DefaultExclude = []string{
	".git", ".DS_Store", ".gitkeep", ".venv", ".idea", ".vscode",
	"node_modules", "__pycache__", "target", "*.pyc", "*.swp", "*~",
}

var knownLanguageNames = map[string]string{
	"de": "Deutsch", "en": "English", "es": "Español", "fr": "Français",
	"it": "Italiano", "ja": "日本語", "ko": "한국어", "pt": "Português", "zh": "中文",
}

var (
	languageCodeRe = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]+)?$`)
	trackIDRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	hexColorRe     = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

// FindRoot walks up from dir until it finds a workshop.yaml.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		if _, err := os.Stat(filepath.Join(cur, FileName)); err == nil {
			return cur, nil
		}
		if cur == filepath.Dir(cur) {
			return "", fmt.Errorf("no %s found in %s or any parent directory (run `wsbuilder init` first)", FileName, abs)
		}
	}
}

// Load reads, completes and validates the configuration of the workshop at root.
func Load(root string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(root, FileName))
	if err != nil {
		return nil, err
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a misspelled setting must not be silently ignored
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(root); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	setDefault := func(field *string, value string) {
		if *field == "" {
			*field = value
		}
	}
	setDefault(&c.ShortTitle, c.Title)
	setDefault(&c.Branch, "main")
	setDefault(&c.Paths.Modules, "modules")
	setDefault(&c.Paths.Exercises, "exercises")
	setDefault(&c.Paths.Images, "modules/images")
	setDefault(&c.Paths.Public, "public")
	setDefault(&c.Paths.Theme, "theme")
	setDefault(&c.Paths.Output, "dist")

	if len(c.Languages) == 0 {
		c.Languages = []Language{{Code: "en"}}
	}
	for i := range c.Languages {
		setDefault(&c.Languages[i].Name, knownLanguageNames[c.Languages[i].Code])
		setDefault(&c.Languages[i].Name, c.Languages[i].Code)
	}
	for i := range c.Tracks {
		setDefault(&c.Tracks[i].Name, c.Tracks[i].ID)
	}

	if len(c.Solutions) == 0 {
		c.Solutions = []string{"solution"}
	}

	c.URL = strings.TrimRight(c.URL, "/")
	if c.URL != "" {
		c.URL += "/"
	}
	c.Repository = strings.TrimRight(c.Repository, "/")
	c.Exclude = append(append([]string{}, DefaultExclude...), c.Exclude...)
}

func (c *Config) validate(root string) error {
	if strings.TrimSpace(c.Title) == "" {
		return errors.New("title is required")
	}
	for name, value := range map[string]string{"url": c.URL, "repository": c.Repository} {
		if value == "" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be an absolute http(s) URL, got %q", name, value)
		}
	}

	seen := map[string]bool{}
	for _, lang := range c.Languages {
		if !languageCodeRe.MatchString(lang.Code) {
			return fmt.Errorf("invalid language code %q", lang.Code)
		}
		if seen[lang.Code] {
			return fmt.Errorf("language %q is listed twice", lang.Code)
		}
		seen[lang.Code] = true
	}

	seen = map[string]bool{}
	for _, track := range c.Tracks {
		if !trackIDRe.MatchString(track.ID) {
			return fmt.Errorf("invalid track id %q (use lowercase letters, digits, - and _)", track.ID)
		}
		if seen[track.ID] {
			return fmt.Errorf("track %q is listed twice", track.ID)
		}
		seen[track.ID] = true
	}

	for name, value := range map[string]string{"theme.primary": c.Theme.Primary, "theme.accent": c.Theme.Accent} {
		if value != "" && !hexColorRe.MatchString(value) {
			return fmt.Errorf("%s must be a hex color like #621773, got %q", name, value)
		}
	}

	return c.validatePaths(root)
}

func (c *Config) validatePaths(root string) error {
	sources := map[string]string{
		"paths.modules":   c.Paths.Modules,
		"paths.exercises": c.Paths.Exercises,
		"paths.images":    c.Paths.Images,
		"paths.public":    c.Paths.Public,
		"paths.theme":     c.Paths.Theme,
	}
	for name, p := range sources {
		if !filepath.IsLocal(filepath.FromSlash(p)) {
			return fmt.Errorf("%s must stay inside the workshop, got %q", name, p)
		}
	}
	return c.ValidateOutput(root, c.Paths.Output)
}

// ValidateOutput refuses output directories that a build, which replaces the
// directory wholesale, could use to destroy sources.
func (c *Config) ValidateOutput(root, output string) error {
	out := filepath.Clean(filepath.FromSlash(output))
	if !filepath.IsLocal(out) || out == "." {
		return fmt.Errorf("output must be a subdirectory of the workshop, got %q", output)
	}
	if strings.Split(out, string(filepath.Separator))[0] == ".git" {
		return fmt.Errorf("output cannot be inside .git, got %q", output)
	}
	for _, src := range []string{c.Paths.Modules, c.Paths.Exercises, c.Paths.Images, c.Paths.Public, c.Paths.Theme} {
		s := filepath.Clean(filepath.FromSlash(src))
		if within(out, s) || within(s, out) {
			return fmt.Errorf("output %q overlaps the source directory %q", output, src)
		}
	}
	return nil
}

// within reports whether path a is b or is inside b. Both are relative.
func within(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator))
}

// DefaultLanguage is the language published at the site root.
func (c *Config) DefaultLanguage() Language { return c.Languages[0] }

// Language looks up a configured language by code.
func (c *Config) Language(code string) (Language, bool) {
	for _, lang := range c.Languages {
		if lang.Code == code {
			return lang, true
		}
	}
	return Language{}, false
}

// Localize resolves the translatable settings for a language.
func (c *Config) Localize(lang Language) Localized {
	pick := func(translated, fallback string) string {
		if translated != "" {
			return translated
		}
		return fallback
	}
	facts := c.Facts
	if len(lang.Facts) > 0 {
		facts = lang.Facts
	}
	title := pick(lang.Title, c.Title)
	return Localized{
		Title:       title,
		ShortTitle:  pick(lang.ShortTitle, pick(lang.Title, c.ShortTitle)),
		Description: pick(lang.Description, c.Description),
		AuthorRole:  pick(lang.AuthorRole, c.Author.Role),
		AuthorBio:   pick(lang.AuthorBio, c.Author.Bio),
		Facts:       facts,
	}
}

// IsSolution reports whether a slash-separated path inside an exercise
// directory belongs to the solution.
func (c *Config) IsSolution(rel string) bool {
	top, _, _ := strings.Cut(rel, "/")
	for _, pattern := range c.Solutions {
		if ok, _ := filepath.Match(pattern, top); ok {
			return true
		}
	}
	return false
}

// IsExcluded reports whether a slash-separated relative path has a component
// matching the exclude list.
func (c *Config) IsExcluded(rel string) bool {
	for _, part := range strings.Split(rel, "/") {
		for _, pattern := range c.Exclude {
			if ok, _ := filepath.Match(pattern, part); ok {
				return true
			}
		}
	}
	return false
}
