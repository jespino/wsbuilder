package site

import (
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// isExternal reports whether a destination leaves the site or stays on the
// current page, so that it needs no rewriting.
func isExternal(dest string) bool {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "//") {
		return true
	}
	u, err := url.Parse(dest)
	return err == nil && u.Scheme != ""
}

// splitDest separates a path from its query and fragment.
func splitDest(dest string) (p, suffix string) {
	if i := strings.IndexAny(dest, "?#"); i >= 0 {
		return dest[:i], dest[i:]
	}
	return dest, ""
}

// linkResolver rewrites the links of one page. Authors write links as they
// work on GitHub, between markdown files, and they become links between pages.
type linkResolver struct {
	b        *builder
	file     *source
	selected map[string]bool // modules in this build, by slug
	known    map[string]bool // all modules, by slug
	prefix   string          // from the page to the site root: "" or "../"
}

func (r *linkResolver) link(dest string) (string, bool) {
	if isExternal(dest) {
		return dest, true
	}
	p, suffix := splitDest(dest)
	name := path.Base(p)

	if strings.HasSuffix(name, ".md") {
		if strings.EqualFold(name, "README.md") || homeFileRe.MatchString(name) {
			return "index.html" + suffix, true
		}
		if m := moduleFileRe.FindStringSubmatch(name); m != nil {
			slug := m[1] + "-" + m[2]
			switch {
			case r.selected[slug]:
				return slug + ".html" + suffix, true
			case r.known[slug]:
				// Disabled in this build: a link to it would be dangling.
				return "", false
			}
		}
	}

	modules := r.b.cfg.Paths.Modules
	rel := path.Join(modules, p)
	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		r.b.errorfAt(r.file, dest, "broken link %q: it points outside the workshop", dest)
		return dest, true
	}
	if _, err := os.Stat(filepath.Join(r.b.root, filepath.FromSlash(rel))); err != nil {
		r.b.errorfAt(r.file, dest, "broken link %q: no such module or file", dest)
		return dest, true
	}

	// Files in public/ are published at the site root, so link to that copy.
	if published, ok := strings.CutPrefix(rel, r.b.cfg.Paths.Public+"/"); ok {
		return r.prefix + published + suffix, true
	}

	// Anything else that exists is only in the repository, so link there.
	if r.b.cfg.Repository == "" {
		r.b.warnfAt(r.file, dest, "link %q points at a repository file but `repository` is not configured", dest)
		return dest, true
	}
	return r.b.cfg.Repository + "/tree/" + r.b.cfg.Branch + "/" + rel + suffix, true
}

// image rewrites an image path so that it works from any language directory.
// Paths are written relative to the modules directory, or as images/<file>.
func (r *linkResolver) image(src string) string {
	if isExternal(src) {
		return src
	}
	p, suffix := splitDest(src)
	rel, ok := r.b.imagePath(p)
	if !ok {
		r.b.errorfAt(r.file, src, "broken image %q: not found in %s/", src, r.b.cfg.Paths.Images)
		return src
	}
	return r.prefix + "images/" + rel + suffix
}

// imagePath finds a referenced image and returns its path inside the images
// directory. It accepts both images/x.png and ../../images/x.png, so that a
// path which previews correctly on GitHub also builds.
func (b *builder) imagePath(p string) (string, bool) {
	clean := path.Clean(p)
	for strings.HasPrefix(clean, "../") {
		clean = strings.TrimPrefix(clean, "../")
	}
	dir := b.cfg.Paths.Images
	for _, prefix := range []string{dir + "/", "images/"} {
		if rest, ok := strings.CutPrefix(clean, prefix); ok {
			clean = rest
			break
		}
	}
	full := filepath.Join(b.root, filepath.FromSlash(dir), filepath.FromSlash(clean))
	if !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", false
	}
	if info, err := os.Stat(full); err != nil || info.IsDir() {
		return "", false
	}
	return clean, true
}
