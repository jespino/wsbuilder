package site

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"strings"
)

// sitemapEntry is one indexable page. Alternates lists every language version,
// itself included, and is empty for a single-language site.
type sitemapEntry struct {
	URL        string
	Alternates []alternate
}

// alternate is one language version of a page.
type alternate struct {
	Lang string
	URL  string
}

// absURL returns the public URL of a page path relative to the site root, or
// an empty string when no site URL is configured.
func (b *builder) absURL(page string) string {
	if b.cfg.URL == "" {
		return ""
	}
	if page == "index.html" {
		page = ""
	}
	return b.cfg.URL + strings.TrimSuffix(page, "index.html")
}

// jsonLD renders structured data. encoding/json escapes <, > and &, so the
// result cannot break out of its script element.
func jsonLD(data map[string]any) template.JS {
	data["@context"] = "https://schema.org"
	out, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return template.JS(out)
}

func (b *builder) person() map[string]any {
	person := map[string]any{"@type": "Person", "name": b.cfg.Author.Name}
	var sameAs []string
	for _, link := range b.cfg.Author.Links {
		sameAs = append(sameAs, link.URL)
	}
	if len(sameAs) > 0 {
		person["sameAs"] = sameAs
	}
	return person
}

func (b *builder) courseData(title, description, lang, url string) map[string]any {
	data := map[string]any{
		"@type":       "Course",
		"name":        title,
		"description": description,
		"inLanguage":  lang,
	}
	if url != "" {
		data["url"] = url
	}
	if b.cfg.Author.Name != "" {
		data["author"] = b.person()
		data["provider"] = b.person()
	}
	return data
}

func (b *builder) moduleData(title, description, lang, url, courseTitle, courseURL string, position int) map[string]any {
	course := map[string]any{"@type": "Course", "name": courseTitle}
	if courseURL != "" {
		course["url"] = courseURL
	}
	data := map[string]any{
		"@type":                "LearningResource",
		"name":                 title,
		"description":          description,
		"inLanguage":           lang,
		"position":             position,
		"learningResourceType": "Workshop module",
		"isPartOf":             course,
	}
	if url != "" {
		data["url"] = url
	}
	if b.cfg.Author.Name != "" {
		data["author"] = b.person()
	}
	return data
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func renderSitemap(entries []sitemapEntry) []byte {
	var b bytes.Buffer
	b.WriteString(xml.Header)
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">` + "\n")
	for _, entry := range entries {
		fmt.Fprintf(&b, "  <url>\n    <loc>%s</loc>\n", xmlEscape(entry.URL))
		for _, alt := range entry.Alternates {
			fmt.Fprintf(&b, "    <xhtml:link rel=\"alternate\" hreflang=\"%s\" href=\"%s\"/>\n", xmlEscape(alt.Lang), xmlEscape(alt.URL))
		}
		b.WriteString("  </url>\n")
	}
	b.WriteString("</urlset>\n")
	return b.Bytes()
}

func (b *builder) renderRobots() []byte {
	out := "User-agent: *\nAllow: /\n"
	if b.cfg.URL != "" {
		out += "\nSitemap: " + b.cfg.URL + "sitemap.xml\n"
	}
	return []byte(out)
}
