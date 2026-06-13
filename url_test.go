package main

import (
	"crypto/tls"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// newReq builds a minimal request for baseURL tests. isTLS marks a
// TLS-terminated connection; proto and fwdHost set the forwarded headers
// a reverse proxy would add.
func newReq(host string, isTLS bool, proto, fwdHost string) *http.Request {
	r := &http.Request{Host: host, Header: http.Header{}}
	if isTLS {
		r.TLS = &tls.ConnectionState{}
	}
	if proto != "" {
		r.Header.Set("X-Forwarded-Proto", proto)
	}
	if fwdHost != "" {
		r.Header.Set("X-Forwarded-Host", fwdHost)
	}
	return r
}

func TestBaseURL(t *testing.T) {
	long := strings.Repeat("a", 300)
	tests := []struct {
		name    string
		host    string
		isTLS   bool
		proto   string
		fwdHost string
		want    string
	}{
		{"plain http in dev", "localhost:7777", false, "", "", "http://localhost:7777"},
		{"direct TLS without proxy", "rulette.party", true, "", "", "https://rulette.party"},
		{"proxy sets forwarded proto", "10.0.0.11:7777", false, "https", "", "https://10.0.0.11:7777"},
		{"forwarded host preferred over r.Host", "10.0.0.11:7777", false, "https", "rulette.party", "https://rulette.party"},
		{"comma-joined proto uses first token", "rulette.party", false, "https, http", "", "https://rulette.party"},
		{"comma-joined forwarded host uses first token", "rulette.party", false, "https", "rulette.party, evil.com", "https://rulette.party"},
		{"unknown scheme ignored", "rulette.party", false, "gopher", "", "http://rulette.party"},
		{"uppercase scheme normalized", "rulette.party", false, "HTTPS", "", "https://rulette.party"},
		{"smuggled path in forwarded host rejected", "rulette.party", false, "https", "evil.com/phish", "https://rulette.party"},
		{"whitespace forwarded host rejected", "rulette.party", false, "https", "evil com", "https://rulette.party"},
		{"bracketed IPv6 host", "[2001:db8::1]:8080", false, "", "", "http://[2001:db8::1]:8080"},
		{"overlong r.Host yields empty base", long, false, "https", "", ""},
		{"overlong forwarded host falls back to r.Host", "rulette.party", false, "https", long + ".com", "https://rulette.party"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := baseURL(newReq(tt.host, tt.isTLS, tt.proto, tt.fwdHost))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestValidHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"hostname", "rulette.party", true},
		{"hostname with port", "localhost:7777", true},
		{"uppercase", "RULETTE.PARTY", true},
		{"bracketed IPv6 with port", "[::1]:7777", true},
		{"bracketed IPv6 without port", "[2001:db8::1]", true},
		{"empty", "", false},
		{"bare port, empty host", ":80", false},
		{"non-numeric port", "host:abc", false},
		{"userinfo", "user@host", false},
		{"embedded path", "evil.com/phish", false},
		{"embedded scheme", "http://evil.com", false},
		{"whitespace", "evil com", false},
		{"overlong hostname", strings.Repeat("a", 254), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, validHost(tt.host))
		})
	}
}
