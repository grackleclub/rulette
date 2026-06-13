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

// sharedPartials are parsed into every template so their {{define}}
// blocks are callable from any page or fragment: currently just the
// footer.
var sharedPartials = []string{
	"static/html/tmpl.footer.html",
}

// pagePartials hold {{define}} blocks only full pages need: the link-preview
// meta tags (og:image and friends) and the page-load notice popup. They are
// parsed on demand, so the frequently polled HTMX fragments don't read and
// parse them on every render.
var pagePartials = []string{
	"static/html/tmpl.preview.html",
	"static/html/tmpl.notice.html",
}

// renderTemplate executes a template that needs no absolute base URL,
// such as an HTMX fragment or an in-game view. Full pages that emit
// link-preview tags use renderPage instead.
func renderTemplate(ctx context.Context, w io.Writer, path string, data any) error {
	return renderPage(ctx, w, path, "", false, data)
}

// renderPage parses the named template (with the shared partials) and
// executes it against data, writing to w. base is the absolute
// "scheme://host" a template reads via {{ baseURL }} to build
// link-preview links; pass "" when the template emits none. Set fullPage
// when rendering a full page, so the page-only partials (link-preview meta
// tags and the notice popup) are parsed; leave it false for fragments to
// skip that read. Wraps the work in a "template.render" span tagged with
// the path so handler traces show template time as a child span.
func renderPage(ctx context.Context, w io.Writer, path, base string, fullPage bool, data any) error {
	_, span := otel.Tracer(otelScope).Start(ctx, "template.render")
	defer span.End()
	span.SetAttributes(attribute.String("template", path))
	tmpl, err := readParse(static, path, base, fullPage)
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
// {{ baseURL }}. When fullPage is set, the page-only partials (link-preview
// meta tags and the notice popup) are parsed too, so a full page can call them.
func readParse(fs embed.FS, path, base string, fullPage bool) (*template.Template, error) {
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
	partials := sharedPartials
	if fullPage {
		partials = append(partials, pagePartials...)
	}
	for _, p := range partials {
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
