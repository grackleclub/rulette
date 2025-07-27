package main

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"text/template"
)

// TODO: start using this for all static files
func staticHandler(w http.ResponseWriter, r *http.Request) {
	slog.Info("request for static resource", "path", r.URL.Path)
	http.FileServer(http.FS(static)).ServeHTTP(w, r)
}

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
