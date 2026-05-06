package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"
)

const defaultLocale = "en"

// translations is locale -> key -> value, populated by loadTranslations
// at startup and read-only thereafter.
var translations = map[string]map[string]string{}

type ctxLocaleKey struct{}

func init() {
	if err := loadTranslations(translationsFS); err != nil {
		panic(fmt.Sprintf("load translations: %v", err))
	}
}

// loadTranslations reads every tx/*.json file from fs and registers
// each as a locale named after the file's basename (e.g. tx/en.json
// becomes locale "en").
func loadTranslations(fs embed.FS) error {
	entries, err := fs.ReadDir("tx")
	if err != nil {
		return fmt.Errorf("read tx dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := fs.ReadFile(path.Join("tx", e.Name()))
		if err != nil {
			return fmt.Errorf("read %s: %w", e.Name(), err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(raw, &m); err != nil {
			return fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		locale := strings.TrimSuffix(e.Name(), ".json")
		translations[locale] = m
	}
	if _, ok := translations[defaultLocale]; !ok {
		return fmt.Errorf("missing default locale %q", defaultLocale)
	}
	return nil
}

// Tx looks up key for the given locale, falling back to the default
// locale, and finally to the key itself so that missing entries are
// loud but non-fatal.
func Tx(locale, key string) string {
	if m, ok := translations[locale]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	if m, ok := translations[defaultLocale]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return key
}

// Locales returns the registered locale codes in sorted order.
func Locales() []string {
	out := make([]string, 0, len(translations))
	for k := range translations {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// LocaleFromContext returns the locale stored on ctx by i18nMW,
// falling back to the default locale.
func LocaleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxLocaleKey{}).(string); ok && v != "" {
		return v
	}
	return defaultLocale
}

// WithLocale returns a new context carrying locale.
func WithLocale(ctx context.Context, locale string) context.Context {
	return context.WithValue(ctx, ctxLocaleKey{}, locale)
}

// detectLocale picks the request locale by precedence:
// 1. lang cookie (the persisted toggle choice)
// 2. Accept-Language header (first tag's primary subtag)
// 3. defaultLocale
// A candidate is only honored if it's a registered locale.
func detectLocale(r *http.Request) string {
	if c, err := r.Cookie("lang"); err == nil {
		if _, ok := translations[c.Value]; ok {
			return c.Value
		}
	}
	if h := r.Header.Get("Accept-Language"); h != "" {
		first := strings.SplitN(h, ",", 2)[0]
		first = strings.SplitN(first, ";", 2)[0]
		first = strings.ToLower(strings.TrimSpace(first))
		first = strings.SplitN(first, "-", 2)[0]
		if _, ok := translations[first]; ok {
			return first
		}
	}
	return defaultLocale
}
