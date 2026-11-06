// Command wsbuilder creates workshops from markdown and publishes them as
// static websites.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/jespino/workshops-framework/internal/config"
	"github.com/jespino/workshops-framework/internal/dev"
	"github.com/jespino/workshops-framework/internal/scaffold"
	"github.com/jespino/workshops-framework/internal/site"
	"github.com/jespino/workshops-framework/internal/theme"
)

const usage = `wsbuilder creates workshops from markdown files.

Usage:
  wsbuilder <command> [arguments]

Commands:
  init <directory>    Create a new workshop
  new <title>         Add a module to the workshop
  build [directory]   Generate the static website
  watch [directory]   Rebuild on every change and serve with live reload
  check [directory]   Validate the workshop without writing anything
  clean [directory]   Remove the generated website
  themes              List the available themes
  eject [directory]   Copy the current theme into the workshop to customize it
  version             Print the version

Run "wsbuilder <command> -h" for the options of a command.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		if !errors.Is(err, errReported) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

// errReported means the details have already been printed.
var errReported = errors.New("reported")

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	command, args := args[0], args[1:]
	switch command {
	case "init":
		return cmdInit(args)
	case "new":
		return cmdNew(args)
	case "build":
		return cmdBuild(args)
	case "watch":
		return cmdWatch(args)
	case "check":
		return cmdCheck(args)
	case "clean":
		return cmdClean(args)
	case "themes":
		return cmdThemes()
	case "eject":
		return cmdEject(args)
	case "version", "--version", "-v":
		fmt.Println("wsbuilder", version())
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", command, usage)
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

// parse accepts flags before and after positional arguments, which the flag
// package alone does not: `wsbuilder build . --drafts` should just work.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, errReported
			}
			return nil, errReported
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func newFlags(name, synopsis string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: wsbuilder %s\n", synopsis)
		fs.PrintDefaults()
	}
	return fs
}

// rootFrom finds the workshop from an optional directory argument. It searches
// parent directories, so commands work from anywhere inside a workshop.
func rootFrom(positional []string) (string, error) {
	switch len(positional) {
	case 0:
		return config.FindRoot(".")
	case 1:
		return config.FindRoot(positional[0])
	}
	return "", fmt.Errorf("expected at most one directory, got %q", strings.Join(positional, " "))
}

func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// printDiags writes warnings and errors, and returns the number of warnings.
func printDiags(w io.Writer, report *site.Report) int {
	warnings := 0
	for _, d := range report.Diags {
		if d.Error {
			fmt.Fprintf(w, "error: %s\n", d)
		} else {
			warnings++
			fmt.Fprintf(w, "warning: %s\n", d)
		}
	}
	return warnings
}

func relative(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

func cmdInit(args []string) error {
	fs := newFlags("init", "init <directory> [options]")
	title := fs.String("title", "", "workshop title (default: from the directory name)")
	author := fs.String("author", "", "author name (default: git config user.name)")
	themeName := fs.String("theme", theme.DefaultName, `site theme, see "wsbuilder themes"`)
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errReported
	}

	created, skipped, err := scaffold.Init(scaffold.InitOptions{Dir: positional[0], Title: *title, Author: *author, Theme: *themeName})
	if err != nil {
		return err
	}
	for _, file := range created {
		fmt.Println("  created", filepath.Join(positional[0], file))
	}
	for _, file := range skipped {
		fmt.Println("  kept existing", filepath.Join(positional[0], file))
	}
	fmt.Printf("\nYour workshop is ready. Next:\n\n  cd %s\n  wsbuilder watch\n\n", positional[0])
	return nil
}

func cmdNew(args []string) error {
	fs := newFlags("new", `new "<title>" [options]`)
	part := fs.String("part", "", "part of the workshop the module belongs to")
	exercise := fs.Bool("exercise", false, "also create the exercise directory")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errReported
	}
	root, err := config.FindRoot(".")
	if err != nil {
		return err
	}
	created, err := scaffold.NewModule(scaffold.NewModuleOptions{
		Root: root, Title: positional[0], Part: *part, Exercise: *exercise,
	})
	if err != nil {
		return err
	}
	for _, file := range created {
		fmt.Println("  created", file)
	}
	return nil
}

// selection registers the flags that choose which modules are built.
type selection struct {
	only, skip *string
}

func selectionFlags(fs *flag.FlagSet) selection {
	return selection{
		only: fs.String("only", "", "comma-separated modules to include, such as 01,03-parser"),
		skip: fs.String("skip", "", "comma-separated modules to leave out"),
	}
}

func cmdBuild(args []string) error {
	fs := newFlags("build", "build [directory] [options]")
	sel := selectionFlags(fs)
	drafts := fs.Bool("drafts", false, "include draft modules")
	output := fs.String("output", "", "output directory inside the workshop (default: paths.output)")
	strict := fs.Bool("strict", false, "fail on warnings too")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	root, err := rootFrom(positional)
	if err != nil {
		return err
	}

	start := time.Now()
	report, err := site.Build(site.Options{
		Root: root, Output: *output, Drafts: *drafts,
		Only: splitList(*sel.only), Skip: splitList(*sel.skip),
	})
	warnings := printDiags(os.Stderr, report)
	if err != nil {
		if errors.Is(err, site.ErrInvalid) {
			fmt.Fprintf(os.Stderr, "\nbuild failed: %d error(s)\n", report.Errors())
			return errReported
		}
		return err
	}
	fmt.Printf("built %d modules in %d language(s): %d pages in %s (%s)\n",
		report.Modules, report.Languages, report.Pages, relative(report.Output), time.Since(start).Round(time.Millisecond))
	if *strict && warnings > 0 {
		fmt.Fprintf(os.Stderr, "failed: %d warning(s) with --strict\n", warnings)
		return errReported
	}
	return nil
}

func cmdCheck(args []string) error {
	fs := newFlags("check", "check [directory] [options]")
	sel := selectionFlags(fs)
	strict := fs.Bool("strict", false, "fail on warnings too")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	root, err := rootFrom(positional)
	if err != nil {
		return err
	}

	report, err := site.Build(site.Options{
		Root: root, DryRun: true, Drafts: true,
		Only: splitList(*sel.only), Skip: splitList(*sel.skip),
	})
	warnings := printDiags(os.Stderr, report)
	if err != nil && !errors.Is(err, site.ErrInvalid) {
		return err
	}
	if report.Errors() > 0 || (*strict && warnings > 0) {
		fmt.Fprintf(os.Stderr, "\n%d error(s), %d warning(s)\n", report.Errors(), warnings)
		return errReported
	}
	fmt.Printf("ok: %d modules in %d language(s), %d warning(s)\n", report.Modules, report.Languages, warnings)
	return nil
}

func cmdWatch(args []string) error {
	fs := newFlags("watch", "watch [directory] [options]")
	sel := selectionFlags(fs)
	port := fs.Int("port", 8000, "port to serve on")
	host := fs.String("host", "127.0.0.1", "address to serve on")
	noServe := fs.Bool("no-serve", false, "only rebuild, without the web server")
	noDrafts := fs.Bool("no-drafts", false, "leave out draft modules")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	root, err := rootFrom(positional)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return dev.Run(ctx, dev.Options{
		Build: site.Options{
			Root: root, Drafts: !*noDrafts,
			Only: splitList(*sel.only), Skip: splitList(*sel.skip),
		},
		Host:    *host,
		Port:    *port,
		NoServe: *noServe,
		Log:     func(format string, a ...any) { fmt.Printf(format+"\n", a...) },
		OnBuild: func(report *site.Report, err error, took time.Duration) {
			printDiags(os.Stderr, report)
			switch {
			case errors.Is(err, site.ErrInvalid):
				fmt.Fprintf(os.Stderr, "[%s] build failed: %d error(s)\n", time.Now().Format("15:04:05"), report.Errors())
			case err != nil:
				fmt.Fprintf(os.Stderr, "[%s] build failed: %v\n", time.Now().Format("15:04:05"), err)
			default:
				fmt.Printf("[%s] built %d pages in %s\n", time.Now().Format("15:04:05"), report.Pages, took.Round(time.Millisecond))
			}
		},
	})
}

func cmdClean(args []string) error {
	fs := newFlags("clean", "clean [directory]")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	root, err := rootFrom(positional)
	if err != nil {
		return err
	}
	dir, err := site.Clean(root)
	if err != nil {
		return err
	}
	fmt.Println("removed", relative(dir))
	return nil
}

func cmdThemes() error {
	for _, info := range theme.Available {
		fmt.Printf("  %-12s %s\n", info.Name, info.Description)
	}
	fmt.Println("\nSelect one with theme.name in workshop.yaml, or with `wsbuilder init --theme`.")
	return nil
}

func cmdEject(args []string) error {
	fs := newFlags("eject", "eject [directory]")
	positional, err := parse(fs, args)
	if err != nil {
		return err
	}
	root, err := rootFrom(positional)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, filepath.FromSlash(cfg.Paths.Theme))
	written, err := theme.Eject(cfg.Theme.Name, dir)
	if err != nil {
		return err
	}
	for _, file := range written {
		fmt.Println("  created", filepath.Join(relative(dir), file))
	}
	if len(written) == 0 {
		fmt.Println("the theme directory already has every file")
	}
	return nil
}
