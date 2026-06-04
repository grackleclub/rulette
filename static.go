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

// renderTemplate parses the named template (with shared footer) and
// executes it against data, writing to w. Wraps the parse+execute in
// a "template.render" span tagged with the template path so handler
// traces show template time as a child span.
func renderTemplate(ctx context.Context, w io.Writer, path string, data any) error {
	_, span := otel.Tracer(otelScope).Start(ctx, "template.render")
	defer span.End()
	span.SetAttributes(attribute.String("template", path))
	tmpl, err := readParse(static, path)
	if err != nil {
		return fmt.Errorf("read parse %q: %w", path, err)
	}
	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute %q: %w", path, err)
	}
	return nil
}

// readParse reads a template file from an embedded filesystem,
// parses it together with the shared footer, and returns the
// resulting *template.Template or any error.
func readParse(fs embed.FS, path string) (*template.Template, error) {
	f, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q from embed.FS: %w", path, err)
	}
	footer, err := fs.ReadFile("static/html/tmpl.footer.html")
	if err != nil {
		return nil, fmt.Errorf("read footer template: %w", err)
	}
	name := filepath.Base(path)
	funcs := template.FuncMap{
		"version": func() string { return version },
	}
	tmpl, err := template.New(name).Funcs(funcs).Parse(string(f))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}
	if _, err := tmpl.Parse(string(footer)); err != nil {
		return nil, fmt.Errorf("parse footer template: %w", err)
	}
	return tmpl, nil
}
