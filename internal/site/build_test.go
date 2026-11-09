package site

import (
	"archive/zip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jespino/wsbuilder/internal/config"
	"github.com/jespino/wsbuilder/internal/theme"
)

const baseConfig = `title: Test Workshop
url: https://example.com/ws
repository: https://github.com/example/ws
author: {name: Ada}
languages:
  - code: en
  - code: es
    title: Taller de Prueba
tracks:
  - {id: go, name: Go}
  - {id: js, name: JS}
`

// workshop writes files into a new directory. Keys are relative paths.
func workshop(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if _, ok := files["workshop.yaml"]; !ok {
		files["workshop.yaml"] = baseConfig
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func basic() map[string]string {
	return map[string]string{
		"modules/01-one.md":                    "---\ndescription: First.\npart: Basics\n---\n\n# One\n\nGo to [two](02-two.md#deep) or [home](../README.md).\n\n![pic](images/a.png)\n",
		"modules/01-one.es.md":                 "---\npart: Lo básico\n---\n\n# Uno\n\nVe a [dos](02-two.es.md).\n\n![pic](images/a.png)\n",
		"modules/02-two.md":                    "---\npart: Basics\n---\n\n# Two\n\n## Deep\n\nBack to [one](01-one.md).\n",
		"modules/index.md":                     "Intro text.\n\n<!-- modules -->\n\nOutro text.\n",
		"modules/images/a.png":                 "png",
		"exercises/01-one/main.go":             "package main\n",
		"exercises/01-one/solution/main.go":    "package solved\n",
		"exercises/01-one/node_modules/x/x.js": "junk",
		"public/CNAME":                         "example.com\n",
	}
}

func read(t *testing.T, root, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "dist", filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func build(t *testing.T, root string) *Report {
	t.Helper()
	report, err := Build(Options{Root: root})
	if err != nil {
		t.Fatalf("build failed: %v\n%v", err, report.Diags)
	}
	return report
}

func TestBuild(t *testing.T) {
	root := workshop(t, basic())
	report := build(t, root)
	if report.Modules != 2 || report.Pages != 7 {
		t.Errorf("modules=%d pages=%d", report.Modules, report.Pages)
	}

	one := read(t, root, "01-one.html")
	mustContain(t, one,
		`<a href="02-two.html#deep">two</a>`,
		`<a href="index.html">home</a>`,
		`src="images/a.png"`,
		`<title>One — Test Workshop</title>`,
		`<meta name="description" content="First.">`,
		`rel="canonical" href="https://example.com/ws/01-one.html"`,
		`hreflang="es" href="https://example.com/ws/es/01-one.html"`,
		`href="downloads/01-one.zip"`,
		`href="downloads/01-one-solution.zip"`,
		`href="https://github.com/example/ws/tree/main/exercises/01-one"`)

	es := read(t, root, "es/01-one.html")
	mustContain(t, es,
		`<html lang="es"`,
		`src="../images/a.png"`,
		`<a href="02-two.html">dos</a>`,
		`href="../styles.css?v=`,
		`Lo básico`)

	home := read(t, root, "index.html")
	if strings.Index(home, "Intro text") > strings.Index(home, "module-grid") ||
		strings.Index(home, "module-grid") > strings.Index(home, "Outro text") {
		t.Errorf("modules marker ignored:\n%s", home)
	}
	mustContain(t, read(t, root, "es/index.html"), "Taller de Prueba", "Intro text")
	mustContain(t, read(t, root, "CNAME"), "example.com")
	mustContain(t, read(t, root, "robots.txt"), "Sitemap: https://example.com/ws/sitemap.xml")
	mustContain(t, read(t, root, "404.html"), `<base href="https://example.com/ws/">`)
	if _, err := os.Stat(filepath.Join(root, "dist", ".nojekyll")); err != nil {
		t.Error(err)
	}
}

func TestExerciseDownloads(t *testing.T) {
	root := workshop(t, basic())
	build(t, root)

	names := func(file string) string {
		z, err := zip.OpenReader(filepath.Join(root, "dist", "downloads", file))
		if err != nil {
			t.Fatal(err)
		}
		defer z.Close()
		var out []string
		for _, f := range z.File {
			out = append(out, f.Name)
		}
		return strings.Join(out, " ")
	}
	if got := names("01-one.zip"); got != "01-one/main.go" {
		t.Errorf("starter zip has %q", got)
	}
	if got := names("01-one-solution.zip"); got != "01-one/solution/main.go" {
		t.Errorf("solution zip has %q", got)
	}
}

func TestSolutionFilesAreConfigurable(t *testing.T) {
	files := basic()
	files["workshop.yaml"] = baseConfig + "solutions: [solution.go]\n"
	files["exercises/01-one/solution.go"] = "package answer\n"
	root := workshop(t, files)
	build(t, root)

	z, err := zip.OpenReader(filepath.Join(root, "dist", "downloads", "01-one-solution.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if len(z.File) != 1 || z.File[0].Name != "01-one/solution.go" {
		t.Errorf("solution zip: %+v", z.File)
	}
}

func TestBuildsAreReproducible(t *testing.T) {
	root := workshop(t, basic())
	hash := func() string {
		sum := sha256.New()
		_ = filepath.WalkDir(filepath.Join(root, "dist"), func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				data, _ := os.ReadFile(p)
				fmt.Fprintf(sum, "%s %x\n", strings.TrimPrefix(p, root), sha256.Sum256(data))
			}
			return nil
		})
		return fmt.Sprintf("%x", sum.Sum(nil))
	}
	build(t, root)
	first := hash()
	build(t, root)
	if hash() != first {
		t.Error("two builds of the same input differ")
	}
}

func TestMissingTranslationFallsBack(t *testing.T) {
	root := workshop(t, basic())
	report := build(t, root)

	page := read(t, root, "es/02-two.html")
	mustContain(t, page,
		"aún no está traducido",
		// Search engines are sent to the original, as this page duplicates it.
		`rel="canonical" href="https://example.com/ws/02-two.html"`)
	if strings.Contains(page, `hreflang="es" href="https://example.com/ws/es/02-two.html"`) {
		t.Error("a fallback page must not be announced as a translation")
	}
	if strings.Contains(read(t, root, "sitemap.xml"), "/es/02-two.html") {
		t.Error("a fallback page must not be in the sitemap")
	}

	warned := 0
	for _, d := range report.Diags {
		if strings.Contains(d.Msg, "no es translation") {
			warned++
		}
	}
	if warned != 1 {
		t.Errorf("expected one translation warning, got %d: %v", warned, report.Diags)
	}
}

func TestDraftsAndSelection(t *testing.T) {
	files := basic()
	files["modules/02-two.md"] = "---\ndraft: true\n---\n\n# Two\n"
	root := workshop(t, files)

	build(t, root)
	if _, err := os.Stat(filepath.Join(root, "dist", "02-two.html")); err == nil {
		t.Error("draft was published")
	}
	one := read(t, root, "01-one.html")
	if strings.Contains(one, `href="02-two.html`) {
		t.Errorf("link to a disabled module was kept:\n%s", one)
	}
	mustContain(t, one, "Go to two or")

	if _, err := Build(Options{Root: root, Drafts: true}); err != nil {
		t.Fatal(err)
	}
	mustContain(t, read(t, root, "02-two.html"), "Draft")

	if _, err := Build(Options{Root: root, Only: []string{"02"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist", "01-one.html")); err == nil {
		t.Error("--only left another module behind: stale pages must not survive")
	}

	if _, err := Build(Options{Root: root, Only: []string{"99"}}); err == nil {
		t.Error("an unknown selector must fail")
	}
}

func TestMistakesFailTheBuild(t *testing.T) {
	cases := map[string]struct{ file, content string }{
		"broken link":   {"modules/03-x.md", "[x](99-nope.md)\n"},
		"broken image":  {"modules/03-x.md", "![x](images/nope.png)\n"},
		"unknown track": {"modules/03-x.md", "<!-- track:rust -->\n\n<!-- /track -->\n"},
		"bad include":   {"modules/03-x.md", "<!-- include: nope.go -->\n"},
		"frontmatter":   {"modules/03-x.md", "---\ntitel: Typo\n---\n"},
		"unclosed fm":   {"modules/03-x.md", "---\ntitle: X\n"},
		"no exercise":   {"modules/03-x.md", "---\nexercise: nope\n---\n# X\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			files := basic()
			root := workshop(t, files)
			build(t, root)
			before := read(t, root, "01-one.html")

			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(c.file)), []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			report, err := Build(Options{Root: root})
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, diags = %v", err, report.Diags)
			}
			if report.Errors() == 0 {
				t.Error("no diagnostics")
			}
			if read(t, root, "01-one.html") != before {
				t.Error("a failed build changed the published site")
			}
		})
	}
}

func TestDiagnosticsPointAtTheLine(t *testing.T) {
	files := basic()
	files["modules/03-x.md"] = "---\ntitle: X\n---\n\ntext\n\n<!-- track:rust -->\n\n<!-- /track -->\n\n[x](99-nope.md)\n"
	root := workshop(t, files)
	report, _ := Build(Options{Root: root})

	lines := map[int]bool{}
	for _, d := range report.Diags {
		if d.Error {
			lines[d.Line] = true
		}
	}
	if !lines[7] || !lines[11] {
		t.Errorf("wrong lines: %+v", report.Diags)
	}
}

func TestDangerousOutputIsRefused(t *testing.T) {
	for _, output := range []string{".", "..", "../elsewhere", "/tmp/x", "modules", "modules/site", "exercises", ".git"} {
		t.Run(output, func(t *testing.T) {
			root := workshop(t, basic())
			if _, err := Build(Options{Root: root, Output: output}); err == nil {
				t.Fatal("build accepted a dangerous output directory")
			}
			if _, err := os.Stat(filepath.Join(root, "modules", "01-one.md")); err != nil {
				t.Fatal("sources were touched")
			}
			files := basic()
			files["workshop.yaml"] = baseConfig + "paths: {output: \"" + output + "\"}\n"
			root = workshop(t, files)
			if _, err := Clean(root); err == nil {
				t.Fatal("clean accepted a dangerous output directory")
			}
			if _, err := os.Stat(filepath.Join(root, "modules", "01-one.md")); err != nil {
				t.Fatal("sources were removed")
			}
		})
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	root := workshop(t, basic())
	if _, err := Build(Options{Root: root, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist")); err == nil {
		t.Error("check wrote the output directory")
	}
}

func TestConfigTypoIsAnError(t *testing.T) {
	files := basic()
	files["workshop.yaml"] = "title: X\nauthr: {name: Y}\n"
	if _, err := Build(Options{Root: workshop(t, files)}); err == nil {
		t.Error("a misspelled setting must not be ignored")
	}
}

func TestThemeOverrides(t *testing.T) {
	files := basic()
	files["theme/custom.css"] = ".mine{}"
	files["theme/404.html"] = `{{define "main"}}<p>my not found page</p>{{end}}`
	root := workshop(t, files)
	build(t, root)

	mustContain(t, read(t, root, "custom.css"), ".mine{}")
	mustContain(t, read(t, root, "01-one.html"), `href="custom.css?v=`)
	mustContain(t, read(t, root, "404.html"), "my not found page")

	files = basic()
	files["theme/module.html"] = `{{define "main"}}{{.Nope}}{{end}}`
	if _, err := Build(Options{Root: workshop(t, files)}); err == nil {
		t.Error("a theme with a bad field must fail the build")
	}
}

func TestEjectedThemeBuildsTheSameSite(t *testing.T) {
	root := workshop(t, basic())
	build(t, root)
	before := read(t, root, "01-one.html")

	if _, err := theme.Eject(theme.DefaultName, filepath.Join(root, "theme")); err != nil {
		t.Fatal(err)
	}
	build(t, root)
	if read(t, root, "01-one.html") != before {
		t.Error("ejecting the theme changed the output")
	}
}

func withTheme(name string) map[string]string {
	files := basic()
	files["workshop.yaml"] = baseConfig + "theme: {name: " + name + "}\n"
	return files
}

func TestEveryThemeBuildsEveryPage(t *testing.T) {
	for _, info := range theme.Available {
		t.Run(info.Name, func(t *testing.T) {
			files := withTheme(info.Name)
			files["modules/03-three.md"] = "---\ndraft: true\n---\n\n# Three\n\n## Part\n\n<!-- track:go -->\n\n### Go only\n\n<!-- /track -->\n"
			root := workshop(t, files)
			if _, err := Build(Options{Root: root, Drafts: true}); err != nil {
				t.Fatal(err)
			}
			mustContain(t, read(t, root, "01-one.html"),
				"data-progress", "data-complete", "data-module-link", "data-track-select",
				`href="downloads/01-one.zip"`, `class="prose"`, `class="pager-next"`)
			mustContain(t, read(t, root, "02-two.html"), "aria-current=\"page\"")
			mustContain(t, read(t, root, "03-three.html"), `class="toc-level-3 track-scoped" data-track="go"`)
			mustContain(t, read(t, root, "index.html"), "data-module-link", "module-card")
			mustContain(t, read(t, root, "404.html"), "Page not found")
		})
	}
}

func TestThemesLayerOnTheBase(t *testing.T) {
	root := workshop(t, withTheme("internals"))
	build(t, root)

	page := read(t, root, "es/01-one.html")
	styles := strings.Index(page, `href="../styles.css?v=`)
	overlay := strings.Index(page, `href="../theme.css?v=`)
	if styles < 0 || overlay < styles {
		t.Errorf("theme.css must load after styles.css:\n%s", page)
	}
	mustContain(t, page, `class="contents-box"`, "(1 de 2)")

	baseRoot := workshop(t, withTheme(theme.BaseName))
	build(t, baseRoot)
	if read(t, baseRoot, "styles.css") != read(t, root, "styles.css") {
		t.Error("the stylesheet should be shared with the base theme")
	}
	if _, err := os.Stat(filepath.Join(baseRoot, "dist", "theme.css")); err == nil {
		t.Error("the base theme has no theme.css")
	}
}

func TestWorkshopsWithoutAThemeGetTheDefault(t *testing.T) {
	unset := workshop(t, basic())
	build(t, unset)
	explicit := workshop(t, withTheme(theme.DefaultName))
	build(t, explicit)
	for _, file := range []string{"theme.css", "01-one.html"} {
		if read(t, unset, file) != read(t, explicit, file) {
			t.Errorf("%s differs from a workshop naming the default theme", file)
		}
	}
	mustContain(t, read(t, unset, "theme.css"), "The default theme")
}

func TestEjectingAnyThemeBuildsTheSameSite(t *testing.T) {
	for _, info := range theme.Available {
		t.Run(info.Name, func(t *testing.T) {
			root := workshop(t, withTheme(info.Name))
			build(t, root)
			before := read(t, root, "01-one.html")
			if _, err := theme.Eject(info.Name, filepath.Join(root, "theme")); err != nil {
				t.Fatal(err)
			}
			build(t, root)
			if read(t, root, "01-one.html") != before {
				t.Error("ejecting the theme changed the output")
			}
		})
	}
}

// Every theme has to work in both color modes, whichever way the reader gets
// there, and with colors set in workshop.yaml.
func TestThemesHandleDarkModeAndConfiguredColors(t *testing.T) {
	for _, info := range theme.Available {
		if info.Name == theme.BaseName {
			continue
		}
		t.Run(info.Name, func(t *testing.T) {
			root := workshop(t, withTheme(info.Name))
			build(t, root)
			css := read(t, root, "theme.css")
			mustContain(t, css, "prefers-color-scheme: dark", `:root[data-theme="dark"]`)
			if strings.Contains(css, "http://") || strings.Contains(css, "https://") || strings.Contains(css, "@import") {
				t.Error("a theme must not load anything from third parties")
			}
		})
	}
}

func TestFilesInThemeDirectoryBeatTheNamedTheme(t *testing.T) {
	files := withTheme("internals")
	files["theme/theme.css"] = ".overridden{}"
	root := workshop(t, files)
	build(t, root)
	mustContain(t, read(t, root, "theme.css"), ".overridden{}")
}

func TestUnknownThemeIsAnError(t *testing.T) {
	_, err := Build(Options{Root: workshop(t, withTheme("nope"))})
	if err == nil || !strings.Contains(err.Error(), "internals") {
		t.Errorf("err = %v, want the list of themes", err)
	}
}

func TestLinksToPublicFiles(t *testing.T) {
	files := basic()
	files["public/slides.pdf"] = "pdf"
	files["modules/01-one.md"] = "# One\n\n[slides](../public/slides.pdf#page=2)\n"
	files["modules/01-one.es.md"] = "# Uno\n\n[slides](../public/slides.pdf)\n"
	root := workshop(t, files)
	build(t, root)
	mustContain(t, read(t, root, "01-one.html"), `<a href="slides.pdf#page=2">`)
	mustContain(t, read(t, root, "es/01-one.html"), `<a href="../slides.pdf">`)
	mustContain(t, read(t, root, "slides.pdf"), "pdf")
}

// The instructions for agents have to keep up with what the tool accepts.
func TestAgentInstructionsCoverEverything(t *testing.T) {
	data, err := os.ReadFile("../scaffold/files/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)

	var missing []string
	var keys func(reflect.Type)
	keys = func(typ reflect.Type) {
		for i := range typ.NumField() {
			field := typ.Field(i)
			key := field.Tag.Get("yaml")
			if key != "" && !strings.Contains(doc, key) {
				missing = append(missing, key)
			}
			inner := field.Type
			if inner.Kind() == reflect.Slice {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct {
				keys(inner)
			}
		}
	}
	keys(reflect.TypeOf(config.Config{}))
	keys(reflect.TypeOf(frontMatter{}))

	for _, item := range []string{"build", "watch", "check", "new", "clean", "eject", "themes"} {
		if !strings.Contains(doc, "wsbuilder "+item) {
			missing = append(missing, "wsbuilder "+item)
		}
	}
	for _, name := range append(append([]string{theme.BaseTemplate, theme.ThemeCSS, theme.CustomCSS}, theme.Pages...), theme.Assets...) {
		if !strings.Contains(doc, name) {
			missing = append(missing, name)
		}
	}
	for _, alert := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION", "HINT", "SOLUTION"} {
		if !strings.Contains(doc, alert) {
			missing = append(missing, alert)
		}
	}
	if len(missing) > 0 {
		t.Errorf("AGENTS.md does not mention: %s", strings.Join(missing, ", "))
	}
}

func TestTrackParityWarning(t *testing.T) {
	files := basic()
	files["modules/02-two.md"] = "# Two\n\n<!-- track:go -->\n\nonly go\n\n<!-- /track -->\n"
	root := workshop(t, files)
	report := build(t, root)
	for _, d := range report.Diags {
		if strings.Contains(d.Msg, `none for "js"`) {
			return
		}
	}
	t.Errorf("no parity warning: %v", report.Diags)
}

func TestStorageKeyIsPerWorkshop(t *testing.T) {
	root := workshop(t, basic())
	build(t, root)
	mustContain(t, read(t, root, "index.html"), `data-storage="wsb:https-example-com-ws"`)
}
