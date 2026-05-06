package main

import (
	"embed"
	"fmt"
	"html/template"
	"path/filepath"
)

// readParse reads a template file from an embedded filesystem,
// parses it together with the shared footer, and returns the
// resulting *template.Template or any error.
//
// The locale argument binds the template's Tx function so that
// {{Tx "key"}} in templates resolves against the caller's locale.
func readParse(fs embed.FS, path string, locale string) (*template.Template, error) {
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
		"Tx":      func(key string) string { return Tx(locale, key) },
		"locale":  func() string { return locale },
		"locales": Locales,
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
