package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jespino/workshops-framework/internal/site"
)

func TestInitCreatesAWorkshopThatBuilds(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-new-workshop")
	created, _, err := Init(InitOptions{Dir: dir, Author: `Ada "The" L`})
	if err != nil {
		t.Fatal(err)
	}
	if len(created) == 0 {
		t.Fatal("nothing created")
	}

	cfg, err := os.ReadFile(filepath.Join(dir, "workshop.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `title: "My New Workshop"`) {
		t.Errorf("title not filled in:\n%s", cfg)
	}
	for _, name := range []string{".gitignore", ".github/workflows/deploy.yml", "README.md", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			t.Error(err)
		}
	}

	report, err := site.Build(site.Options{Root: dir})
	if err != nil {
		t.Fatalf("starter workshop does not build: %v\n%v", err, report.Diags)
	}
	if report.Modules != 2 {
		t.Errorf("modules = %d", report.Modules)
	}
}

func TestInitWithTheme(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Init(InitOptions{Dir: dir, Author: "A", Theme: "internals"}); err != nil {
		t.Fatal(err)
	}
	report, err := site.Build(site.Options{Root: dir})
	if err != nil {
		t.Fatalf("%v\n%v", err, report.Diags)
	}
	styles, err := os.ReadFile(filepath.Join(dir, "dist", "theme.css"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(styles), "Internals for Interns") {
		t.Error("the internals theme was not selected")
	}

	empty := filepath.Join(t.TempDir(), "new")
	if _, _, err := Init(InitOptions{Dir: empty, Theme: "nope"}); err == nil {
		t.Error("init must refuse an unknown theme")
	}
	if _, err := os.Stat(empty); err == nil {
		t.Error("a refused init must not create anything")
	}
}

func TestInitDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, ".gitignore")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, skipped, err := Init(InitOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 {
		t.Errorf("skipped = %v", skipped)
	}
	if data, _ := os.ReadFile(existing); string(data) != "mine\n" {
		t.Error("existing file overwritten")
	}
	if _, _, err := Init(InitOptions{Dir: dir}); err == nil {
		t.Error("init must refuse an existing workshop")
	}
}

func TestNewModule(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Init(InitOptions{Dir: dir, Author: "A"}); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "workshop.yaml")
	data, _ := os.ReadFile(config)
	data = []byte(strings.Replace(string(data), "  - code: en\n", "  - code: en\n  - code: es\n", 1))
	if err := os.WriteFile(config, data, 0o644); err != nil {
		t.Fatal(err)
	}

	created, err := NewModule(NewModuleOptions{Root: dir, Title: "Going Deeper: Part 2!", Part: "Advanced", Exercise: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"modules/03-going-deeper-part-2.md", "modules/03-going-deeper-part-2.es.md", "exercises/03-going-deeper-part-2/"}
	if strings.Join(created, " ") != strings.Join(want, " ") {
		t.Errorf("created = %v", created)
	}

	if report, err := site.Build(site.Options{Root: dir}); err != nil {
		t.Fatalf("%v\n%v", err, report.Diags)
	}
	if _, err := NewModule(NewModuleOptions{Root: dir, Title: "!!!"}); err == nil {
		t.Error("a title without letters must fail")
	}
}
