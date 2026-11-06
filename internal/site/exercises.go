package site

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
)

// exercise is the downloadable material of a module.
type exercise struct {
	Dir       string   // relative to the workshop root
	Files     []string // relative to Dir, without the solution
	Solutions []string // relative to Dir, the files matched by cfg.Solutions
}

// findExercise locates a module's exercise directory: the one named in its
// frontmatter or, by convention, the one named like the module.
func (b *builder) findExercise(mod *module, src *source) (*exercise, error) {
	name := src.Meta.Exercise
	explicit := name != ""
	if !explicit {
		name = mod.Slug
	}
	rel := path.Join(b.cfg.Paths.Exercises, name)
	dir := filepath.Join(b.root, filepath.FromSlash(rel))

	if !filepath.IsLocal(filepath.FromSlash(rel)) {
		return nil, fmt.Errorf("exercise path %q leaves the workshop", name)
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && !info.IsDir()) {
		if explicit {
			return nil, fmt.Errorf("exercise directory %s not found", rel)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	ex := &exercise{Dir: rel}
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		r = filepath.ToSlash(r)
		if r == "." {
			return nil
		}
		if b.cfg.IsExcluded(r) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if b.cfg.IsSolution(r) {
			ex.Solutions = append(ex.Solutions, r)
		} else {
			ex.Files = append(ex.Files, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(ex.Files)
	sort.Strings(ex.Solutions)
	if len(ex.Files)+len(ex.Solutions) == 0 {
		return nil, nil
	}
	return ex, nil
}

// writeZip packs files into a zip whose entries live under prefix. Entries
// carry no timestamps, so unchanged exercises produce identical downloads.
func writeZip(out, dir, prefix string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, name := range files {
		src := filepath.Join(dir, filepath.FromSlash(name))
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: path.Join(prefix, name), Method: zip.Deflate}
		header.SetMode(info.Mode().Perm())
		w, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, in)
		in.Close()
		if err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
