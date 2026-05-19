package main

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

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
	sessionLimiters = make(map[string]*rate.Limiter)
	ipLimiters      = make(map[string]*rate.Limiter)
	mu              sync.Mutex
	sessionRate     = rate.Every(25 * time.Millisecond) // 40/s per session
	sessionBurst    = 80
	ipRate          = rate.Every(100 * time.Millisecond) // 10/s per IP
	ipBurst         = 30
)

func getSessionLimiter(key string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()
	limiter, exists := sessionLimiters[key]
	if !exists {
		limiter = rate.NewLimiter(sessionRate, sessionBurst)
		sessionLimiters[key] = limiter
	}
	return limiter
}

func getIPLimiter(ip string) *rate.Limiter {
	mu.Lock()
	defer mu.Unlock()
	limiter, exists := ipLimiters[ip]
	if !exists {
		limiter = rate.NewLimiter(ipRate, ipBurst)
		ipLimiters[ip] = limiter
	}
	return limiter
}

func rateMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var limiter *rate.Limiter
		if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
			limiter = getSessionLimiter(c.Value)
		} else {
			ip := r.RemoteAddr
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				ip = host
			}
			limiter = getIPLimiter(ip)
		}
		if !limiter.Allow() {
			log.Warn("rate limit exceeded", "path", r.URL.Path)
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}
