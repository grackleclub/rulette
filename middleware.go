package main

import (
	"net/http"
)

// logMW logs every incoming request.
func logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Info("request received",
			"path", r.URL.Path,
			"method", r.Method,
			"query", r.URL.RawQuery,
			// "remote_addr", r.RemoteAddr,
			// "user_agent", r.UserAgent(),
			// "referer", r.Referer(),
			// "content_length", r.ContentLength,
			// "host", r.Host,
		)
		next.ServeHTTP(w, r)
	})
}
