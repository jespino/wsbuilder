// Package scaffold creates new workshops and new modules.
package scaffold

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/jespino/wsbuilder/internal/config"
	"github.com/jespino/wsbuilder/internal/theme"
)

// The starter files. "all:" keeps the dotfiles. Names ending in .tmpl have
// placeholders filled in and lose that extension.
//
//go:embed all:files
var starter embed.FS

// InitOptions configures a new workshop.
type InitOptions struct {
	Dir    string
	Title  string
	Author string
	Theme  string
}

// Init creates a workshop in dir. It refuses to touch an existing workshop, and
// inside any other existing directory it never overwrites a file.
func Init(opts InitOptions) (created, skipped []string, err error) {
	dir, err := filepath.Abs(opts.Dir)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
		return nil, nil, fmt.Errorf("%s already has a %s", dir, config.FileName)
	}

	themeName := opts.Theme
	if themeName == "" {
		themeName = theme.DefaultName
	}
	if _, err := theme.New(themeName, ""); err != nil {
		return nil, nil, err
	}

	title := opts.Title
	if title == "" {
		title = titleFromDir(filepath.Base(dir))
	}
	author := opts.Author
	if author == "" {
		author = gitUserName()
	}
	if author == "" {
		author = "Your Name"
	}
	replacer := strings.NewReplacer(
		"__TITLE__", strconv.Quote(title),
		"__AUTHOR__", strconv.Quote(author),
		"__PLAIN_TITLE__", title,
		"__THEME__", themeName,
	)

	err = fs.WalkDir(starter, "files", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("files", p)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := starter.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(target, ".tmpl") {
			target = strings.TrimSuffix(target, ".tmpl")
			rel = strings.TrimSuffix(rel, ".tmpl")
			data = []byte(replacer.Replace(string(data)))
		}
		if _, err := os.Stat(target); err == nil {
			skipped = append(skipped, rel)
			return nil
		}
		created = append(created, rel)
		return os.WriteFile(target, data, 0o644)
	})
	return created, skipped, err
}

func titleFromDir(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' || r == ' ' })
	for i, word := range words {
		runes := []rune(word)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	if len(words) == 0 {
		return "My Workshop"
	}
	return strings.Join(words, " ")
}

func gitUserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NewModuleOptions configures a new module.
type NewModuleOptions struct {
	Root     string
	Title    string
	Part     string
	Exercise bool
}

var numberRe = regexp.MustCompile(`^(\d+)-`)

// NewModule adds a module after the existing ones, in every language, and
// returns the files it created.
func NewModule(opts NewModuleOptions) ([]string, error) {
	cfg, err := config.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(opts.Root, filepath.FromSlash(cfg.Paths.Modules))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	next := 1
	for _, entry := range entries {
		if m := numberRe.FindStringSubmatch(entry.Name()); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			if n >= next {
				next = n + 1
			}
		}
	}

	name := slugify(opts.Title)
	if name == "" {
		return nil, errors.New("the title needs at least one letter or digit")
	}
	slug := fmt.Sprintf("%02d-%s", next, name)

	var body strings.Builder
	body.WriteString("---\n")
	fmt.Fprintf(&body, "description: %s\n", strconv.Quote("What this module is about, in one sentence."))
	if opts.Part != "" {
		fmt.Fprintf(&body, "part: %s\n", strconv.Quote(opts.Part))
	}
	body.WriteString("---\n\n")
	fmt.Fprintf(&body, "# %s\n\nIntroduce the idea here.\n\n## Exercise\n\nDescribe what to do.\n", opts.Title)

	var created []string
	for i, lang := range cfg.Languages {
		file := slug + ".md"
		if i > 0 {
			file = slug + "." + lang.Code + ".md"
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body.String()), 0o644); err != nil {
			return created, err
		}
		created = append(created, path.Join(cfg.Paths.Modules, file))
	}

	if opts.Exercise {
		rel := path.Join(cfg.Paths.Exercises, slug)
		exDir := filepath.Join(opts.Root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Join(exDir, "solution"), 0o755); err != nil {
			return created, err
		}
		readme := fmt.Sprintf("# %s\n\nStarter files for this exercise. Put the finished version in `solution/`.\n", opts.Title)
		if err := os.WriteFile(filepath.Join(exDir, "README.md"), []byte(readme), 0o644); err != nil {
			return created, err
		}
		created = append(created, rel+"/")
	}
	return created, nil
}

func slugify(s string) string {
	var b strings.Builder
	dash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}
