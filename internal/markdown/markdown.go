// Package markdown turns a module's markdown into HTML. It is CommonMark plus
// GFM, with the small set of directives workshops need:
//
//	<!-- track:go --> … <!-- /track -->    content for one code track
//	<!-- include: path lines=3-9 -->       a code block read from a real file
//	> [!NOTE] / [!TIP] / [!IMPORTANT] / [!WARNING] / [!CAUTION]   callouts
//	> [!HINT] / [!SOLUTION]                collapsed until the reader asks
//
// Directives are written as HTML comments and GitHub alerts so that a module
// still reads correctly when previewed on GitHub. They are recognized on the
// parsed tree rather than in the text, so examples inside code blocks are safe.
package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

// TrackInfo is a code track the document may refer to.
type TrackInfo struct {
	ID   string
	Name string
}

// Options configures one render.
type Options struct {
	// Root is the directory include paths are relative to.
	Root string
	// Tracks lists the valid track ids.
	Tracks []TrackInfo
	// Labels holds the localized alert titles, keyed by lowercase alert kind.
	Labels map[string]string
	// Link rewrites a link destination. Returning false removes the link and
	// keeps its text. Nil leaves links untouched.
	Link func(dest string) (string, bool)
	// Image rewrites an image source. Nil leaves images untouched.
	Image func(src string) string
	// LineOffset is added to reported line numbers, to account for frontmatter.
	LineOffset int
}

// Heading is one entry of a page's table of contents.
type Heading struct {
	Level int
	Text  string
	ID    string
	Track string // set when the heading only exists in one track
}

// Problem is an authoring mistake found while rendering.
type Problem struct {
	Line int
	Msg  string
}

// Result is a rendered document.
type Result struct {
	HTML string
	// Title is the text of a leading H1, which is removed from HTML so that
	// templates can place it.
	Title string
	// Summary is the text of the first paragraph, for use as a description.
	Summary  string
	Headings []Heading
	// Tracks lists the track ids the document has blocks for.
	Tracks   []string
	Problems []Problem
}

// Render converts markdown to HTML.
func Render(source []byte, opts Options) (*Result, error) {
	result := &Result{}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.Footnote),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(&transformer{opts: opts, result: result}, 100)),
		),
		// Raw HTML stays enabled: the author is trusted, and handouts need it.
		goldmark.WithRendererOptions(
			html.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(&nodeRenderer{labels: opts.Labels}, 100)),
		),
	)

	var out bytes.Buffer
	if err := md.Convert(source, &out); err != nil {
		return nil, fmt.Errorf("rendering markdown: %w", err)
	}
	result.HTML = out.String()
	return result, nil
}

// plainText extracts the text of an inline tree.
func plainText(n ast.Node, source []byte) string {
	var b strings.Builder
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch v := c.(type) {
			case *ast.Text:
				b.Write(v.Segment.Value(source))
				if v.SoftLineBreak() || v.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				b.Write(v.Value)
			case *ast.RawHTML:
				// Tags are not text.
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// summarize shortens text to at most limit runes, cutting at a word boundary.
func summarize(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	cut := string(runes[:limit])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.") + "…"
}

// lineOf returns the 1-based line of a byte offset.
func lineOf(source []byte, offset int) int {
	return bytes.Count(source[:offset], []byte("\n")) + 1
}
