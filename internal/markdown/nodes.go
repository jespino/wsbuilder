package markdown

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	kindTrackStart = ast.NewNodeKind("TrackStart")
	kindTrackEnd   = ast.NewNodeKind("TrackEnd")
	kindAlert      = ast.NewNodeKind("Alert")
	kindInclude    = ast.NewNodeKind("Include")

	fenceTitleRe = regexp.MustCompile(`title=(?:"([^"]*)"|(\S+))`)
)

type trackStart struct {
	ast.BaseBlock
	ID   string
	Name string
}

func (n *trackStart) Kind() ast.NodeKind { return kindTrackStart }
func (n *trackStart) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"ID": n.ID}, nil)
}

type trackEnd struct{ ast.BaseBlock }

func (n *trackEnd) Kind() ast.NodeKind { return kindTrackEnd }
func (n *trackEnd) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type alertBlock struct {
	ast.BaseBlock
	Variant string // note, tip, important, warning, caution, hint, solution
	Title   string // optional custom title
}

func (n *alertBlock) Kind() ast.NodeKind { return kindAlert }
func (n *alertBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Variant": n.Variant}, nil)
}

type includeBlock struct {
	ast.BaseBlock
	Code  string
	Lang  string
	Title string
}

func (n *includeBlock) Kind() ast.NodeKind { return kindInclude }
func (n *includeBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"Title": n.Title}, nil)
}

type nodeRenderer struct {
	labels map[string]string
}

func (r *nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindTrackStart, r.renderTrackStart)
	reg.Register(kindTrackEnd, r.renderTrackEnd)
	reg.Register(kindAlert, r.renderAlert)
	reg.Register(kindInclude, r.renderInclude)
	reg.Register(ast.KindFencedCodeBlock, r.renderFenced)
	reg.Register(ast.KindCodeBlock, r.renderIndented)
}

func (r *nodeRenderer) renderTrackStart(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*trackStart)
		id := html.EscapeString(n.ID)
		fmt.Fprintf(w, "<div class=\"track-block track-scoped\" data-track=\"%s\"><div class=\"track-label\">%s</div>\n", id, html.EscapeString(n.Name))
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderTrackEnd(w util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderAlert(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*alertBlock)
	collapsed := n.Variant == "hint" || n.Variant == "solution"
	switch {
	case entering:
		title := n.Title
		if title == "" {
			title = r.labels[n.Variant]
		}
		if title == "" {
			title = strings.ToUpper(n.Variant[:1]) + n.Variant[1:]
		}
		title = html.EscapeString(title)
		if collapsed {
			fmt.Fprintf(w, "<details class=\"alert alert-%s\"><summary>%s</summary>\n<div class=\"alert-body\">\n", n.Variant, title)
		} else {
			fmt.Fprintf(w, "<aside class=\"alert alert-%s\"><p class=\"alert-title\">%s</p>\n<div class=\"alert-body\">\n", n.Variant, title)
		}
	case collapsed:
		_, _ = w.WriteString("</div></details>\n")
	default:
		_, _ = w.WriteString("</div></aside>\n")
	}
	return ast.WalkContinue, nil
}

func (r *nodeRenderer) renderInclude(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*includeBlock)
		writeCode(w, n.Lang, n.Title, n.Code)
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderFenced(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		n := node.(*ast.FencedCodeBlock)
		title := ""
		if n.Info != nil {
			info := string(n.Info.Segment.Value(source))
			if m := fenceTitleRe.FindStringSubmatch(info); m != nil {
				title = m[1] + m[2]
			}
		}
		writeCode(w, string(n.Language(source)), title, codeText(n.Lines(), source))
	}
	return ast.WalkSkipChildren, nil
}

func (r *nodeRenderer) renderIndented(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		writeCode(w, "", "", codeText(node.Lines(), source))
	}
	return ast.WalkSkipChildren, nil
}

func codeText(lines *text.Segments, source []byte) string {
	var b strings.Builder
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(source))
	}
	return b.String()
}

// writeCode renders a code block. Highlighting happens here, at build time, so
// the published page needs no script or CDN for it. Blocks without a language
// are left alone: they are usually program output.
func writeCode(w util.BufWriter, lang, title, code string) {
	_, _ = w.WriteString(`<div class="code-block">`)
	if title != "" {
		fmt.Fprintf(w, `<div class="code-title">%s</div>`, html.EscapeString(title))
	}
	if lang != "" {
		fmt.Fprintf(w, `<pre class="chroma" data-lang="%s"><code class="language-%s">`, html.EscapeString(lang), html.EscapeString(lang))
	} else {
		_, _ = w.WriteString(`<pre class="chroma"><code>`)
	}
	_, _ = w.WriteString(highlight(lang, code))
	_, _ = w.WriteString("</code></pre></div>\n")
}

func highlight(lang, code string) string {
	lexer := lexers.Get(lang)
	if lang == "" || lexer == nil {
		return html.EscapeString(code)
	}
	tokens, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return html.EscapeString(code)
	}
	var out bytes.Buffer
	formatter := chromahtml.New(chromahtml.WithClasses(true), chromahtml.PreventSurroundingPre(true))
	if err := formatter.Format(&out, styles.Fallback, tokens); err != nil {
		return html.EscapeString(code)
	}
	return out.String()
}
