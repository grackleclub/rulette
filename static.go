package main

import (
	"embed"
	"fmt"
	"path/filepath"
	"text/template"
)

// readParse reads a template file from an embedded filesystem,
// parses it, and returns the resulting *template.Template or any error.
func readParse(fs embed.FS, path string) (*template.Template, error) {
	f, err := fs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file %q from embed.FS: %w", path, err)
	}
	name := filepath.Base(path)
	tmpl, err := template.New(name).Parse(string(f))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", name, err)
	}
	return tmpl, nil
}
