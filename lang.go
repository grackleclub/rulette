package main

import (
	"net/http"
	"time"
)

// langHandler persists the user's locale preference via a cookie.
// On invalid or unknown locales, falls back silently to the default.
func langHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	choice := r.FormValue("lang")
	if _, ok := translations[choice]; !ok {
		choice = defaultLocale
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "lang",
		Value:    choice,
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		SameSite: http.SameSiteLaxMode,
	})
	target := r.Referer()
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
