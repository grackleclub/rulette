package main

import (
	"net/http"
	"strings"
)

// maxlenDNS is the maximum length of a DNS name (RFC 1035). Host values
// are validated against it before going into a URL or a QR code.
const maxlenDNS = 253

// baseURL returns the absolute "scheme://host" for the request, so
// templates and the QR code can build absolute links (link-preview
// crawlers require absolute og:image and og:url). It honors the
// X-Forwarded-Proto and X-Forwarded-Host headers a reverse proxy sets,
// but doesn't trust them blindly: the scheme must be http or https and
// the host must look like a host, so a client reaching the app without a
// header-sanitizing proxy can't poison the absolute URLs.
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	proto := strings.ToLower(firstToken(r.Header.Get("X-Forwarded-Proto")))
	if proto == "http" || proto == "https" {
		scheme = proto
	}
	// Prefer a forwarded host, fall back to the host the server received,
	// but require either to look like a host so an over-long or malformed
	// Host header can't reflect into the absolute URL.
	host := firstToken(r.Header.Get("X-Forwarded-Host"))
	if !validHost(host) {
		host = r.Host
	}
	if !validHost(host) {
		return ""
	}
	return scheme + "://" + host
}

// firstToken returns the first comma-separated value of a header,
// trimmed. Across a proxy chain X-Forwarded-* arrives comma-joined
// ("https, http"); only the left-most value — set by the proxy closest
// to the client — is meaningful, so use it rather than the raw header.
func firstToken(v string) string {
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// validHost reports whether h is a plausible "host" or "host:port" safe
// to drop into an absolute URL: non-empty, within DNS length, and built
// only from hostname and port characters, so a forwarded value can't
// smuggle whitespace, a path, or a second scheme into the URL.
func validHost(h string) bool {
	if h == "" || len(h) > maxlenDNS {
		return false
	}
	for _, c := range h {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '.', c == '-', c == ':',
			c == '[', c == ']':
		default:
			return false
		}
	}
	return true
}
