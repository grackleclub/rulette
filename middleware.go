package main

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// i18nMW resolves the request locale (cookie > Accept-Language > default)
// and stashes it on the request context for handlers and templates.
func i18nMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithLocale(r.Context(), detectLocale(r))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// logMW logs every incoming request.
func logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Debug("request received",
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

var (
	ipLimiters = make(map[string]*rate.Limiter)
	mu         sync.Mutex
	rateLimit  = rate.Every(50 * time.Millisecond)
	burst      = 50
)

func getLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()
	limiter, exists := ipLimiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rateLimit, burst)
		ipLimiters[ip] = limiter
	}
	return limiter
}

func rateMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			ip = host
		}
		limiter := getLimiter(ip)
		if !limiter.Allow() {
			log.Warn("rate limit exceeded", "ip", ip, "path", r.URL.Path)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
