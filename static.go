package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"path/filepath"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// pageData wraps a full page's own data with the absolute base site URL
// (scheme://host) the template needs to build link-preview tags
// (og:image, og:url). Carrying base in the data — rather than a funcmap
// closure — keeps the parsed template free of per-request state, so it
// can be cached and reused (see templates). Fragments that emit no
// preview tags pass their data directly and ignore this wrapper.
type pageData struct {
	BaseURL string
	Data    any
}

// tmplFuncs are the helpers available to every template. They hold no
// per-request state, so a template parsed with them is safe to cache and
// reuse across requests.
var tmplFuncs = template.FuncMap{
	"version": func() string { return version },
	"add":     func(a, b int) int { return a + b },
	"abs": func(n int32) int32 {
		if n < 0 {
			return -n
		}
		return n
	},
	"inGame": func(data any) bool {
		_, ok := data.(state)
		return ok
	},
}

// templates caches parsed *template.Template by path. Parsing lexes the
// source and builds the parse tree; doing it once per template instead
// of once per request takes that work off every render, including hot
// paths like game-state polling. html/template is safe for concurrent
// Execute, so the cached value is shared freely.
var templates sync.Map // path -> *template.Template

// renderTemplate executes the named template (with shared footer)
// against data, writing to w. Wraps execute in a "template.render" span
// tagged with the template path so handler traces show template time as
// a child span. The parsed template is cached after first use.
func renderTemplate(ctx context.Context, w io.Writer, path string, data any) error {
	_, span := otel.Tracer(otelScope).Start(ctx, "template.render")
	defer span.End()
	span.SetAttributes(attribute.String("template", path))
	tmpl, err := pageTemplate(path)
	if err != nil {
		return fmt.Errorf("template %q: %w", path, err)
	}
	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute %q: %w", path, err)
	}
	return nil
}

// pageTemplate returns the cached template for path, parsing it (with
// the shared footer) on first use.
func pageTemplate(path string) (*template.Template, error) {
	if t, ok := templates.Load(path); ok {
		return t.(*template.Template), nil
	}
	t, err := readParse(static, path)
	if err != nil {
		return nil, fmt.Errorf("read parse %q: %w", path, err)
	}
	templates.Store(path, t)
	return t, nil
}

// sharedPartials are parsed into every page so their {{define}} blocks
// are callable from any template: the footer, and the link-preview meta
// tags (og:image and friends) shared by the full pages.
var sharedPartials = []string{
	"static/html/tmpl.footer.html",
	"static/html/tmpl.preview.html",
}

// readParse reads a template file from an embedded filesystem and parses
// it together with the shared partials.
func readParse(fs embed.FS, path string) (*template.Template, error) {
	f, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q from embed.FS: %w", path, err)
	}
	name := filepath.Base(path)
	tmpl, err := template.New(name).Funcs(tmplFuncs).Parse(string(f))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}
	for _, p := range sharedPartials {
		b, err := fs.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read partial %q: %w", p, err)
		}
		if _, err := tmpl.Parse(string(b)); err != nil {
			return nil, fmt.Errorf("parse partial %q: %w", p, err)
		}
	}
	return tmpl, nil
}
