package markdown

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var tracks = []TrackInfo{{ID: "go", Name: "Go"}, {ID: "js", Name: "Node.js"}}

func render(t *testing.T, src string, opts Options) *Result {
	t.Helper()
	if opts.Tracks == nil {
		opts.Tracks = tracks
	}
	r, err := Render([]byte(src), opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustContain(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in:\n%s", want, html)
		}
	}
}

func TestTrackBlocks(t *testing.T) {
	r := render(t, "intro\n\n<!-- track:go -->\n\n## Only Go\n\n**bold** text\n\n<!-- /track -->\n\nafter\n", Options{})
	if len(r.Problems) != 0 {
		t.Fatalf("unexpected problems: %v", r.Problems)
	}
	mustContain(t, r.HTML,
		`<div class="track-block track-scoped" data-track="go"><div class="track-label">Go</div>`,
		`<strong>bold</strong>`)
	if strings.Contains(r.HTML, "<!--") {
		t.Errorf("marker leaked into output:\n%s", r.HTML)
	}
	if len(r.Headings) != 1 || r.Headings[0].Track != "go" {
		t.Errorf("heading not attributed to its track: %+v", r.Headings)
	}
	if len(r.Tracks) != 1 || r.Tracks[0] != "go" {
		t.Errorf("tracks used = %v", r.Tracks)
	}
}

func TestTrackBlockInsideListItem(t *testing.T) {
	r := render(t, "1. step\n\n   <!-- track:go -->\n\n   ```go\n   x := 1\n   ```\n\n   <!-- /track -->\n\n2. next\n", Options{})
	if len(r.Problems) != 0 {
		t.Fatalf("unexpected problems: %v", r.Problems)
	}
	mustContain(t, r.HTML, `data-track="go"`, "<li>")
	if strings.Count(r.HTML, "<ol>") != 1 {
		t.Errorf("list was split:\n%s", r.HTML)
	}
}

func TestDirectivesInCodeAreIgnored(t *testing.T) {
	r := render(t, "```markdown\n<!-- track:nope -->\n<!-- include: missing.txt -->\n```\n", Options{})
	if len(r.Problems) != 0 {
		t.Fatalf("code example treated as a directive: %v", r.Problems)
	}
	mustContain(t, r.HTML, "track:nope")
}

func TestTrackMistakesAreReported(t *testing.T) {
	cases := map[string]string{
		"unknown":   "<!-- track:rust -->\n\n<!-- /track -->\n",
		"unclosed":  "<!-- track:go -->\n\ntext\n",
		"unopened":  "<!-- /track -->\n",
		"nested":    "<!-- track:go -->\n\n<!-- track:js -->\n\n<!-- /track -->\n\n<!-- /track -->\n",
		"crossing":  "<!-- track:go -->\n\n- item\n\n  <!-- /track -->\n",
		"malformed": "<!-- track go -->\n",
		"inline":    "some text <!-- track:go --> more\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			if r := render(t, src, Options{}); len(r.Problems) == 0 {
				t.Errorf("no problem reported, html:\n%s", r.HTML)
			}
		})
	}
}

func TestProblemLineIncludesOffset(t *testing.T) {
	r := render(t, "a\n\n<!-- /track -->\n", Options{LineOffset: 5})
	if len(r.Problems) != 1 || r.Problems[0].Line != 8 {
		t.Errorf("problems = %+v", r.Problems)
	}
}

func TestAlerts(t *testing.T) {
	r := render(t, "> [!WARNING]\n> Be **careful**\n\n> [!HINT] Look closer\n> Second line\n\n> [!SOLUTION]\n>\n> ```go\n> x := 1\n> ```\n\n> plain quote\n", Options{Labels: map[string]string{"warning": "Cuidado", "solution": "Solución"}})
	mustContain(t, r.HTML,
		`<aside class="alert alert-warning"><p class="alert-title">Cuidado</p>`,
		`<strong>careful</strong>`,
		`<details class="alert alert-hint"><summary>Look closer</summary>`,
		`Second line`,
		`<details class="alert alert-solution"><summary>Solución</summary>`,
		`<blockquote>`)
	if strings.Contains(r.HTML, "[!") {
		t.Errorf("marker leaked:\n%s", r.HTML)
	}
}

func TestInclude(t *testing.T) {
	root := t.TempDir()
	body := "package main\n\nfunc main() {\n\tprintln(\"<hi>\")\n}\n"
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	r := render(t, "<!-- include: main.go lines=4 -->\n", Options{Root: root})
	if len(r.Problems) != 0 {
		t.Fatalf("unexpected problems: %v", r.Problems)
	}
	mustContain(t, r.HTML, `class="code-title">main.go<`, `data-lang="go"`, `&lt;hi&gt;`)
	if strings.Contains(r.HTML, "package") || strings.Contains(r.HTML, "\t") {
		t.Errorf("range or dedent not applied:\n%s", r.HTML)
	}

	r = render(t, `<!-- include: main.go lang=text title="custom name" -->`+"\n", Options{Root: root})
	mustContain(t, r.HTML, `custom name`, `package main`)

	for name, src := range map[string]string{
		"missing":  "<!-- include: nope.go -->\n",
		"escape":   "<!-- include: ../secret -->\n",
		"absolute": "<!-- include: /etc/passwd -->\n",
		"range":    "<!-- include: main.go lines=2-99 -->\n",
		"badrange": "<!-- include: main.go lines=abc -->\n",
		"option":   "<!-- include: main.go bogus=1 -->\n",
	} {
		t.Run(name, func(t *testing.T) {
			if r := render(t, src, Options{Root: root}); len(r.Problems) == 0 {
				t.Errorf("no problem reported, html:\n%s", r.HTML)
			}
		})
	}
}

func TestCodeHighlighting(t *testing.T) {
	r := render(t, "```go title=\"a b.go\"\nfunc main() {}\n```\n\n```\n<plain> output\n```\n", Options{})
	mustContain(t, r.HTML, `class="code-title">a b.go<`, `<span class="kd">func</span>`, `&lt;plain&gt; output`)
}

func TestLinksAndImages(t *testing.T) {
	r := render(t, "[keep](a.md) and [drop *me*](b.md)\n\n![alt](x.png)\n", Options{
		Link: func(dest string) (string, bool) {
			return strings.Replace(dest, ".md", ".html", 1), dest == "a.md"
		},
		Image: func(src string) string { return "../images/" + src },
	})
	mustContain(t, r.HTML, `<a href="a.html">keep</a>`, `drop <em>me</em>`, `src="../images/x.png"`, `loading="lazy"`)
	if strings.Contains(r.HTML, "b.html") {
		t.Errorf("link not removed:\n%s", r.HTML)
	}
}

func TestMetadata(t *testing.T) {
	r := render(t, "# Hello `world`\n\nFirst *paragraph*\nwraps.\n\n## Two\n\n### Three\n\n#### Four\n", Options{})
	if r.Title != "Hello world" {
		t.Errorf("title = %q", r.Title)
	}
	if strings.Contains(r.HTML, "<h1") {
		t.Errorf("leading h1 not removed:\n%s", r.HTML)
	}
	if r.Summary != "First paragraph wraps." {
		t.Errorf("summary = %q", r.Summary)
	}
	if len(r.Headings) != 2 || r.Headings[0].ID != "two" {
		t.Errorf("headings = %+v", r.Headings)
	}
}

func TestSummaryIsCut(t *testing.T) {
	r := render(t, strings.Repeat("word ", 100), Options{})
	if n := len([]rune(r.Summary)); n > summaryLimit+1 || !strings.HasSuffix(r.Summary, "d…") {
		t.Errorf("summary (%d runes) = %q", n, r.Summary)
	}
}
