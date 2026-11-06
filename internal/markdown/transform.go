package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var (
	trackOpenRe  = regexp.MustCompile(`^<!--\s*track:\s*([A-Za-z0-9_-]+)\s*-->$`)
	trackCloseRe = regexp.MustCompile(`^<!--\s*/track\s*-->$`)
	includeRe    = regexp.MustCompile(`^<!--\s*include:\s*(\S+)((?:\s+[a-z]+=(?:"[^"]*"|[^\s"]+))*)\s*-->$`)
	directiveRe  = regexp.MustCompile(`^<!--\s*(/?track\b|include\b)`)
	includeArgRe = regexp.MustCompile(`([a-z]+)=(?:"([^"]*)"|([^\s"]+))`)
	alertRe      = regexp.MustCompile(`(?i)^\[!(NOTE|TIP|IMPORTANT|WARNING|CAUTION|HINT|SOLUTION)\][ \t]*(.*)$`)
)

const summaryLimit = 160

// transformer rewrites the parsed tree: it resolves directives, alerts, links
// and images, and collects the page metadata.
type transformer struct {
	opts   Options
	result *Result
	source []byte
}

func (t *transformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	t.source = reader.Source()

	// Rewriting while walking confuses the walker, so take a snapshot first.
	var nodes []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			nodes = append(nodes, n)
		}
		return ast.WalkContinue, nil
	})

	var open *trackStart
	var openLine int
	used := map[string]bool{}

	for _, n := range nodes {
		switch v := n.(type) {
		case *ast.HTMLBlock:
			raw := strings.TrimSpace(htmlBlockText(v, t.source))
			line := t.blockLine(v)
			switch {
			case trackOpenRe.MatchString(raw):
				id := trackOpenRe.FindStringSubmatch(raw)[1]
				track, ok := t.track(id)
				if !ok {
					t.problem(line, "unknown track %q (known tracks: %s)", id, t.trackIDs())
				}
				if open != nil {
					t.problem(line, "track blocks cannot nest: %q is still open from line %d", open.ID, openLine)
					continue
				}
				start := &trackStart{ID: id, Name: track.Name}
				replace(v, start)
				open, openLine = start, line
				if !used[id] {
					used[id] = true
					t.result.Tracks = append(t.result.Tracks, id)
				}
			case trackCloseRe.MatchString(raw):
				if open == nil {
					t.problem(line, "<!-- /track --> without a matching opening marker")
					continue
				}
				if open.Parent() != v.Parent() {
					t.problem(line, "track block opened on line %d closes at a different nesting level", openLine)
				}
				replace(v, &trackEnd{})
				open = nil
			case includeRe.MatchString(raw):
				t.include(v, includeRe.FindStringSubmatch(raw), line)
			case directiveRe.MatchString(raw):
				t.problem(line, "malformed directive: %s", raw)
			}

		case *ast.RawHTML:
			var b strings.Builder
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				b.Write(seg.Value(t.source))
			}
			if directiveRe.MatchString(b.String()) {
				line := 0
				if v.Segments.Len() > 0 {
					line = lineOf(t.source, v.Segments.At(0).Start) + t.opts.LineOffset
				}
				t.problem(line, "directive must be on its own line, separated from surrounding text: %s", b.String())
			}

		case *ast.Blockquote:
			t.alert(v)

		case *ast.Heading:
			if v.Parent() == ast.Node(doc) && v.Level >= 2 && v.Level <= 3 {
				h := Heading{Level: v.Level, Text: plainText(v, t.source)}
				if id, ok := v.AttributeString("id"); ok {
					if b, ok := id.([]byte); ok {
						h.ID = string(b)
					}
				}
				if open != nil {
					h.Track = open.ID
				}
				t.result.Headings = append(t.result.Headings, h)
			}

		case *ast.Paragraph:
			if t.result.Summary == "" && open == nil && v.Parent() == ast.Node(doc) {
				t.result.Summary = summarize(plainText(v, t.source), summaryLimit)
			}

		case *ast.Link:
			if t.opts.Link != nil {
				dest, keep := t.opts.Link(string(v.Destination))
				if keep {
					v.Destination = []byte(dest)
				} else {
					unwrap(v)
				}
			}

		case *ast.Image:
			if t.opts.Image != nil {
				v.Destination = []byte(t.opts.Image(string(v.Destination)))
			}
			v.SetAttributeString("loading", []byte("lazy"))
		}
	}

	if open != nil {
		t.problem(openLine, "track block %q is never closed", open.ID)
	}

	// A leading H1 becomes the page title, placed by the template.
	if first := doc.FirstChild(); first != nil {
		if h, ok := first.(*ast.Heading); ok && h.Level == 1 {
			t.result.Title = plainText(h, t.source)
			doc.RemoveChild(doc, h)
		}
	}
}

func (t *transformer) problem(line int, format string, args ...any) {
	t.result.Problems = append(t.result.Problems, Problem{Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (t *transformer) track(id string) (TrackInfo, bool) {
	for _, track := range t.opts.Tracks {
		if track.ID == id {
			return track, true
		}
	}
	return TrackInfo{ID: id, Name: id}, false
}

func (t *transformer) trackIDs() string {
	if len(t.opts.Tracks) == 0 {
		return "none configured"
	}
	ids := make([]string, len(t.opts.Tracks))
	for i, track := range t.opts.Tracks {
		ids[i] = track.ID
	}
	return strings.Join(ids, ", ")
}

// htmlBlockText returns the raw text of an HTML block. Goldmark stores the line
// that ends the block separately from the others.
func htmlBlockText(n *ast.HTMLBlock, source []byte) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(source))
	}
	if n.HasClosure() {
		b.Write(n.ClosureLine.Value(source))
	}
	return b.String()
}

func (t *transformer) blockLine(n *ast.HTMLBlock) int {
	offset := 0
	if n.Lines().Len() > 0 {
		offset = n.Lines().At(0).Start
	} else if n.HasClosure() {
		offset = n.ClosureLine.Start
	}
	return lineOf(t.source, offset) + t.opts.LineOffset
}

// alert turns a blockquote starting with a [!KIND] marker into a callout.
func (t *transformer) alert(quote *ast.Blockquote) {
	para, ok := quote.FirstChild().(*ast.Paragraph)
	if !ok || para.Lines().Len() == 0 {
		return
	}
	first := para.Lines().At(0)
	match := alertRe.FindStringSubmatch(strings.TrimSpace(string(first.Value(t.source))))
	if match == nil {
		return
	}

	// Drop the inline nodes of the marker line; whatever follows is the body.
	for c := para.FirstChild(); c != nil; {
		next := c.NextSibling()
		endOfLine := false
		if txt, ok := c.(*ast.Text); ok {
			endOfLine = txt.SoftLineBreak() || txt.HardLineBreak()
		}
		para.RemoveChild(para, c)
		if endOfLine {
			break
		}
		c = next
	}
	if para.ChildCount() == 0 {
		quote.RemoveChild(quote, para)
	}

	alert := &alertBlock{Variant: strings.ToLower(match[1]), Title: match[2]}
	for c := quote.FirstChild(); c != nil; {
		next := c.NextSibling()
		quote.RemoveChild(quote, c)
		alert.AppendChild(alert, c)
		c = next
	}
	quote.Parent().ReplaceChild(quote.Parent(), quote, alert)
}

// include replaces a directive with the content of a file.
func (t *transformer) include(n *ast.HTMLBlock, match []string, line int) {
	rel := filepath.FromSlash(match[1])
	if !filepath.IsLocal(rel) {
		t.problem(line, "include path must stay inside the workshop: %s", match[1])
		return
	}

	args := map[string]string{}
	for _, arg := range includeArgRe.FindAllStringSubmatch(match[2], -1) {
		switch arg[1] {
		case "lines", "lang", "title":
			args[arg[1]] = arg[2] + arg[3]
		default:
			t.problem(line, "unknown include option %q (use lines, lang or title)", arg[1])
		}
	}

	data, err := os.ReadFile(filepath.Join(t.opts.Root, rel))
	if err != nil {
		t.problem(line, "cannot include %s: file not found", match[1])
		return
	}

	code := strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if spec := args["lines"]; spec != "" {
		code, err = selectLines(code, spec)
		if err != nil {
			t.problem(line, "include %s: %v", match[1], err)
			return
		}
	}

	lang := args["lang"]
	if lang == "" {
		if lexer := lexers.Match(filepath.Base(rel)); lexer != nil {
			lang = strings.ToLower(lexer.Config().Name)
		}
	}
	title := args["title"]
	if title == "" {
		title = match[1]
	}
	replace(n, &includeBlock{Code: code + "\n", Lang: lang, Title: title})
}

// selectLines keeps a 1-based inclusive line range ("3", "3-9" or "3-") and
// removes the indentation the selected lines have in common.
func selectLines(code, spec string) (string, error) {
	lines := strings.Split(code, "\n")
	from, to := spec, spec
	if a, b, ok := strings.Cut(spec, "-"); ok {
		from, to = a, b
	}
	if to == "" {
		to = strconv.Itoa(len(lines))
	}
	start, err1 := strconv.Atoi(from)
	end, err2 := strconv.Atoi(to)
	if err1 != nil || err2 != nil || start < 1 || end < start {
		return "", fmt.Errorf("invalid lines=%q (use lines=3, lines=3-9 or lines=3-)", spec)
	}
	if end > len(lines) {
		return "", fmt.Errorf("lines=%s is past the end of the file (%d lines)", spec, len(lines))
	}
	return dedent(lines[start-1 : end]), nil
}

func dedent(lines []string) string {
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || width < indent {
			indent = width
		}
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		if indent > 0 && len(line) >= indent {
			line = line[indent:]
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}

func replace(old, with ast.Node) {
	old.Parent().ReplaceChild(old.Parent(), old, with)
}

// unwrap replaces a node with its children.
func unwrap(n ast.Node) {
	parent := n.Parent()
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		n.RemoveChild(n, c)
		parent.InsertBefore(parent, n, c)
		c = next
	}
	parent.RemoveChild(parent, n)
}
