package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"path/filepath"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// sharedPartials are parsed into every page so their {{define}} blocks
// are callable from any template: the footer, and the link-preview meta
// tags (og:image and friends) shared by the full pages.
var sharedPartials = []string{
	"static/html/tmpl.footer.html",
	"static/html/tmpl.preview.html",
}

// renderTemplate executes a template that needs no absolute base URL,
// such as an HTMX fragment or an in-game view. Full pages that emit
// link-preview tags use renderPage instead.
func renderTemplate(ctx context.Context, w io.Writer, path string, data any) error {
	return renderPage(ctx, w, path, "", data)
}

// renderPage parses the named template (with the shared partials) and
// executes it against data, writing to w. base is the absolute
// "scheme://host" a template reads via {{ baseURL }} to build
// link-preview links; pass "" when the template emits none. Wraps the
// work in a "template.render" span tagged with the path so handler
// traces show template time as a child span.
func renderPage(ctx context.Context, w io.Writer, path, base string, data any) error {
	_, span := otel.Tracer(otelScope).Start(ctx, "template.render")
	defer span.End()
	span.SetAttributes(attribute.String("template", path))
	tmpl, err := readParse(static, path, base)
	if err != nil {
		return fmt.Errorf("read parse %q: %w", path, err)
	}
	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute %q: %w", path, err)
	}
	return nil
}

// readParse reads a template from the embedded filesystem and parses it
// together with the shared partials. base is exposed to the template as
// {{ baseURL }}.
func readParse(fs embed.FS, path, base string) (*template.Template, error) {
	f, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q from embed.FS: %w", path, err)
	}
	name := filepath.Base(path)
	funcs := template.FuncMap{
		"version": func() string { return version },
		"baseURL": func() string { return base },
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
	tmpl, err := template.New(name).Funcs(funcs).Parse(string(f))
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
