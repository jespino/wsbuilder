# wsbuilder — Workshop Builder

`wsbuilder` turns a folder of markdown files into a workshop website: module pages with
progress tracking, code tracks, translations, exercise downloads and hints. The result is
a plain static site that search engines can index and any host can serve.

```bash
go install github.com/jespino/wsbuilder/cmd/wsbuilder@latest

wsbuilder init my-workshop                      # or --theme internals
cd my-workshop
wsbuilder watch        # http://127.0.0.1:8000, reloads as you save
```

## A workshop

```
my-workshop/
  workshop.yaml             title, author, languages, tracks, theme...
  AGENTS.md                 instructions for coding agents working on the workshop
  modules/
    index.md                optional text for the landing page
    01-introduction.md      one file per module; the number sets the order
    01-introduction.es.md   its translation
    images/                 images used by the modules
  exercises/
    02-first-exercise/      files for the module with the same name
      main.go
      solution/             left out of the starter download
  public/                   optional, copied as is to the site root (CNAME, favicon)
  theme/                    optional, overrides of the theme's files
  dist/                     the generated site
```

## Commands

| Command | What it does |
|---|---|
| `wsbuilder init <dir>` | Create a workshop. `--title`, `--author`, `--theme` |
| `wsbuilder new "<title>"` | Add the next module, in every language. `--part`, `--exercise` |
| `wsbuilder watch [dir]` | Rebuild on change and serve with live reload. Includes drafts. `--port`, `--host`, `--no-serve`, `--no-drafts` |
| `wsbuilder build [dir]` | Generate the site. `--drafts`, `--output`, `--strict` |
| `wsbuilder check [dir]` | Validate everything and write nothing. `--strict` fails on warnings too, which suits CI |
| `wsbuilder clean [dir]` | Remove the generated site |
| `wsbuilder themes` | List the available themes |
| `wsbuilder eject [dir]` | Copy the current theme into `theme/` to edit it |

`build`, `check` and `watch` also take `--only 01,03` and `--skip 05-parser`, by number or
full name, to cut a shorter version of a workshop. Links to modules left out of a build
turn into plain text.

## Writing modules

A module is markdown with optional frontmatter. Everything is optional, and the first `#`
heading is the title unless `title` says otherwise.

```markdown
---
title: Manual Metrics           # shown instead of the heading
description: One sentence.      # under the title, on the card, and for search engines
part: Instrumenting             # consecutive modules with the same part are grouped
duration: 45 min
draft: true                     # left out of `build` unless --drafts
exercise: shared-lab            # exercise directory, when not named like the module
---

# Manual Metrics
```

Link between modules as you would on GitHub, `[next](02-next.md#section)`, and they become
links between pages. A file in `public/` is linked relative to the module, as in
`[slides](../public/slides.pdf)`. A translated file inherits whatever frontmatter it leaves out.

GitHub-flavored markdown works: tables, task lists (which readers can tick, and which stay
ticked), footnotes and raw HTML. Code is highlighted at build time, and a block with no
language is left alone, which suits program output. A fence can have a title:
` ```go title="main.go" `.

Workshops get these additions. They all preview sensibly on GitHub, and examples inside
code blocks are left alone.

**Callouts**

```markdown
> [!NOTE]
> Also TIP, IMPORTANT, WARNING and CAUTION.

> [!HINT] Stuck on the loop?
> HINT and SOLUTION stay collapsed until the reader opens them.
```

**Code tracks**: one module for several languages, with a switcher in the navigation bar.
Declare `tracks` in `workshop.yaml`. Anything outside a block is shared by all tracks.

```markdown
<!-- track:go -->

Go-only text and code.

<!-- /track -->
```

**Includes**: show a real file, so the text can't drift from the code. The path is relative
to the workshop, and `lines`, `lang` and `title` are optional.

```markdown
<!-- include: exercises/02-first/main.go lines=10-24 -->
```

**Landing page**: `modules/index.md` appears below the title. A line with
`<!-- modules -->` places the module list inside it.

## Configuration

`workshop.yaml` is checked strictly, so a misspelled setting is an error. Only `title` is
required.

```yaml
title: Go Mechanical Sympathy Workshop
short_title: Go Mechanical Sympathy    # for the navigation bar
description: What participants will learn.

url: https://example.github.io/my-workshop/   # for canonical URLs and the sitemap
repository: https://github.com/example/my-workshop
branch: main
image: images/social.png               # social sharing image

author:
  name: Your Name
  role: What you do
  bio: A sentence or two.
  avatar: images/author.webp
  links:
    - { label: GitHub, url: https://github.com/example }

facts:                                 # shown on the landing page
  - { label: Format, value: Theory + labs }

languages:                             # the first one is the default
  - code: en
  - code: es
    title: Mi Workshop                 # also short_title, description, author_role,
    ui:                                # author_bio, facts
      start: Empezar                   # any interface text, see internal/site/ui.go

tracks:
  - { id: go, name: Go }
  - { id: python, name: Python / Flask }

theme:
  name: default                        # see `wsbuilder themes`
  primary: "#621773"                   # optional, replaces the theme's colors
  accent: "#e94600"

solutions: [solution, solution.go]     # what counts as the solution in an exercise
exclude: ["*.bench.txt"]               # kept out of downloads, on top of the defaults

paths:                                 # the defaults
  modules: modules
  exercises: exercises
  images: modules/images
  public: public
  theme: theme
  output: dist
```

Interface text ships in English and Spanish. Other languages show English until you
translate it with `ui`.

## Working with coding agents

`init` adds an `AGENTS.md` that explains how to work on the workshop: where modules,
images, exercises and translations go, the syntax, the rules and step-by-step recipes.
An existing `AGENTS.md` is left alone, and it doesn't update itself when `wsbuilder` does.

## When something is wrong

Broken links and images, unknown or unbalanced tracks, failed includes and bad frontmatter
stop the build, with `file:line`. A failed build leaves the previous site in place, and
`watch` shows the error in the browser until you fix it.

Warnings don't stop it. They cover missing translations, which show the original text with
a notice, and modules where only some tracks have a block.

## Publishing

`dist/` is a complete static site with no server code, and links are relative, so it works
at any path, even opened from disk. `init` adds a GitHub Pages workflow: enable Pages with
"GitHub Actions" as the source, and every push to `main` deploys.

Each page is complete HTML, so crawlers need no JavaScript. With `url` set, the site gets
canonical URLs, `hreflang` alternates and a `sitemap.xml`. Every page also has Open Graph
tags and schema.org data, and `robots.txt` and `404.html` are generated. Published pages
load nothing from third parties, so they work offline, on conference wifi included.

## Themes

| Theme | Look |
|---|---|
| `default` | Bold cards, a dark navbar and a purple to orange gradient |
| `simple` | Sidebar, table of contents and quiet module cards, for long workshops |
| `internals` | A single reading column, inspired by [Internals for Interns](https://internals-for-interns.com/) |

Select one with `theme.name` in `workshop.yaml`, or with `wsbuilder init --theme`. Every
theme has a light and a dark mode. `default` and `internals` prefer fonts like Lato and
Inter, but only if the reader already has them, as loading web fonts would break the
promise of no third-party requests.

To adjust a theme, from least to most work:

1. Set `theme.primary` and `theme.accent`.
2. Add `theme/custom.css`, loaded last. The CSS variables at the top of `styles.css` control
   colors, fonts and spacing.
3. Replace any single file in `theme/`: `base.html`, `home.html`, `module.html`, `404.html`,
   `styles.css`, `theme.css` or `app.js`. `wsbuilder eject` gives you all of them to start
   from.

Each file resolves in this order: the workshop's `theme/`, the theme in `theme.name`, then
`simple`, which is complete and is the base of the others. Templates are Go `html/template`, their data is the `Page` struct in
`internal/site/pages.go`, and `base.html` defines the pieces a module page can reuse.

To add a built-in theme, create `internal/theme/themes/<name>/` with only the files that
differ, usually a `theme.css` on top of the shared stylesheet, and list it in
`theme.Available`. A theme with no sidebar can reuse the `contents-box` piece from
`base.html`, and needs a dark mode: the tests check for one.

## Development

```bash
make test
make build     # bin/wsbuilder
```

| Package | Purpose |
|---|---|
| `cmd/wsbuilder` | The command line |
| `internal/config` | `workshop.yaml` |
| `internal/markdown` | Markdown to HTML, and the directives |
| `internal/site` | The build: content, links, pages, SEO, downloads |
| `internal/theme` | Theme resolution, and the built-in themes in `themes/` |
| `internal/dev` | `watch`: rebuilds, the dev server and live reload |
| `internal/scaffold` | `init` and `new`, with starter files in `files/`, `AGENTS.md` among them |
