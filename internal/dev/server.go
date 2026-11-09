// Package dev implements `wsbuilder watch`: it rebuilds the site when a file
// changes and, unless told otherwise, serves it with live reload. The reload
// script is added as pages are served, so it never reaches a published build.
package dev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jespino/wsbuilder/internal/site"
)

const (
	pollInterval = 300 * time.Millisecond
	eventsPath   = "/__wsbuilder/events"
)

// Options configures a watch session.
type Options struct {
	Build   site.Options
	Host    string
	Port    int
	NoServe bool
	Log     func(format string, args ...any)
	// OnBuild receives every finished build, to report its diagnostics.
	OnBuild func(*site.Report, error, time.Duration)
}

type server struct {
	opts Options

	mu       sync.Mutex
	clients  map[chan event]struct{}
	output   string
	buildErr string // the failure of the last build, shown in the browser
}

type event struct {
	Kind string `json:"kind"` // "reload" or "error"
	Msg  string `json:"msg,omitempty"`
}

// Run watches until ctx is done. A build that fails does not stop the session:
// fixing the mistake is exactly what the author will do next.
func Run(ctx context.Context, opts Options) error {
	s := &server{opts: opts, clients: map[chan event]struct{}{}}
	s.output = filepath.Join(opts.Build.Root, "dist")
	s.rebuild()

	if opts.NoServe {
		opts.Log("watching for changes, press Ctrl-C to stop")
		s.watch(ctx)
		return nil
	}

	listener, err := net.Listen("tcp", net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)))
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(eventsPath, s.events)
	mux.HandleFunc("/", s.files)
	srv := &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}

	go s.watch(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	opts.Log("serving at http://%s, press Ctrl-C to stop", listener.Addr())
	if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) rebuild() {
	start := time.Now()
	report, err := site.Build(s.opts.Build)
	s.opts.OnBuild(report, err, time.Since(start))

	s.mu.Lock()
	defer s.mu.Unlock()
	if report != nil && report.Output != "" {
		s.output = report.Output
	}
	if err == nil {
		s.buildErr = ""
		s.broadcast(event{Kind: "reload"})
		return
	}
	s.buildErr = describe(report, err)
	s.broadcast(event{Kind: "error", Msg: s.buildErr})
}

// describe gives the text of a failed build.
func describe(report *site.Report, err error) string {
	var lines []string
	if report != nil {
		for _, d := range report.Diags {
			if d.Error {
				lines = append(lines, d.String())
			}
		}
	}
	if len(lines) == 0 {
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "\n")
}

// watch polls the workshop. Polling needs no dependency, watches new
// directories for free, and is not fooled by editors that save by replacing the
// file, which is what breaks most inode-based watchers.
func (s *server) watch(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	last := s.snapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if current := s.snapshot(); current != last {
				last = current
				s.rebuild()
				last = s.snapshot()
			}
		}
	}
}

// snapshot fingerprints every source file.
func (s *server) snapshot() string {
	s.mu.Lock()
	output := s.output
	s.mu.Unlock()

	root := s.opts.Build.Root
	var sum strings.Builder
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if p != root && (strings.HasPrefix(name, ".") || isOutput(p, output) || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "~") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(&sum, "%s|%d|%d\n", p, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return sum.String()
}

// broadcast must be called with the lock held.
func (s *server) broadcast(e event) {
	for ch := range s.clients {
		select {
		case ch <- e:
		default: // that client already has an event pending
		}
	}
}

func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")

	ch := make(chan event, 1)
	s.mu.Lock()
	s.clients[ch] = struct{}{}
	if s.buildErr != "" {
		ch <- event{Kind: "error", Msg: s.buildErr}
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	for {
		select {
		case e := <-ch:
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// files serves the output directory, adding the reload script to HTML.
func (s *server) files(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	output, buildErr := s.output, s.buildErr
	s.mu.Unlock()

	w.Header().Set("Cache-Control", "no-store")
	root := http.Dir(output)

	name := path.Clean("/" + r.URL.Path)
	if strings.HasSuffix(r.URL.Path, "/") {
		name = path.Join(name, "index.html")
	}

	f, err := root.Open(name)
	if err == nil {
		defer f.Close()
		if info, statErr := f.Stat(); statErr == nil && info.IsDir() {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusFound)
			return
		}
		if !strings.HasSuffix(name, ".html") {
			http.ServeContent(w, r, name, time.Time{}, f)
			return
		}
		writePage(w, f, http.StatusOK)
		return
	}

	if notFound, err := root.Open("/404.html"); err == nil {
		defer notFound.Close()
		writePage(w, notFound, http.StatusNotFound)
		return
	}

	// No site yet: the very first build failed.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, "<!doctype html><title>Build failed</title><body><pre>%s</pre>%s</body>", html.EscapeString(buildErr), reloadScript)
}

func writePage(w http.ResponseWriter, f io.Reader, status int) {
	content, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if i := bytes.LastIndex(content, []byte("</body>")); i >= 0 {
		content = append(content[:i:i], append([]byte(reloadScript), content[i:]...)...)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(content)
}

const reloadScript = `<script>
(function () {
  var banner;
  function clear() { if (banner) { banner.remove(); banner = null; } }
  function show(message) {
    clear();
    banner = document.createElement("pre");
    banner.textContent = "Build failed\n\n" + message;
    banner.style.cssText = "position:fixed;inset:auto 16px 16px 16px;z-index:1000;margin:0;padding:16px;" +
      "max-height:50vh;overflow:auto;white-space:pre-wrap;background:#3b0d12;color:#ffd9dc;" +
      "border:1px solid #d6336c;border-radius:10px;font:14px/1.5 ui-monospace,monospace";
    document.body.appendChild(banner);
  }
  var connected = false;
  function connect() {
    var events = new EventSource("` + eventsPath + `");
    events.onopen = function () {
      if (connected) location.reload(); // the server came back
      connected = true;
    };
    events.onmessage = function (message) {
      var event = JSON.parse(message.data);
      if (event.kind === "reload") location.reload();
      if (event.kind === "error") show(event.msg);
    };
  }
  connect();
})();
</script>
`

// isOutput reports whether a directory is the output or a build's working copy.
func isOutput(dir, output string) bool {
	return dir == output || dir == output+".tmp" || dir == output+".old"
}
