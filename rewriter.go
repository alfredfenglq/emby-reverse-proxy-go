package main

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
)

func rewriteResponseHeaders(resp *http.Response, baseURL string) {
	if loc := resp.Header.Get("Location"); loc != "" {
		resp.Header.Set("Location", rewriteSingleURL(loc, baseURL))
	}
	if cl := resp.Header.Get("Content-Location"); cl != "" {
		resp.Header.Set("Content-Location", rewriteSingleURL(cl, baseURL))
	}
}

var rewritableTypes = []string{
	"application/json",
	"text/html",
	"text/xml",
	"text/plain",
	"application/xml",
	"application/xhtml",
	"text/javascript",
	"application/javascript",
}

func shouldRewriteBody(contentType string) bool {
	ct := strings.ToLower(contentType)
	for _, t := range rewritableTypes {
		if strings.Contains(ct, t) {
			return true
		}
	}
	return false
}

var httpScheme = []byte("http://")
var httpsScheme = []byte("https://")

// rewriteBody scans for all http:// and https:// URLs in the body and
// rewrites them to proxy URLs. Uses bytes.Index for fast searching —
// no regex, no url.Parse per match.
func rewriteBody(body []byte, baseURL string) []byte {
	// Fast check: if no "http" in body at all, skip entirely.
	if !bytes.Contains(body, []byte("http")) {
		return body
	}

	var out []byte
	i := 0
	for i < len(body) {
		// Use bytes.Index to find next "http://" or "https://" — this is
		// optimized in Go runtime (uses SIMD on amd64) and far faster
		// than our byte-at-a-time loop.
		remaining := body[i:]
		httpPos := bytes.Index(remaining, httpScheme)
		httpsPos := bytes.Index(remaining, httpsScheme)

		// Pick whichever comes first
		pos := -1
		schemeLen := 0
		if httpPos >= 0 && (httpsPos < 0 || httpPos <= httpsPos) {
			// "http://" found at or before "https://"
			// But check if this is actually "https://" (httpPos points to 'h' of "https://")
			if httpsPos >= 0 && httpsPos == httpPos {
				pos = httpsPos
				schemeLen = 8 // len("https://")
			} else {
				pos = httpPos
				schemeLen = 7 // len("http://")
			}
		} else if httpsPos >= 0 {
			pos = httpsPos
			schemeLen = 8
		}

		if pos < 0 {
			if out == nil {
				return body // fast path: no URLs at all
			}
			out = append(out, remaining...)
			break
		}

		// Lazy-init output buffer
		if out == nil {
			out = make([]byte, 0, len(body)+len(body)/8)
		}

		// Copy bytes before this URL
		out = append(out, remaining[:pos]...)

		// Find end of URL
		urlStart := i + pos
		urlEnd := urlStart + schemeLen
		for urlEnd < len(body) && !isURLTerminator(body[urlEnd]) {
			urlEnd++
		}

		// Rewrite inline — no url.Parse, no alloc
		raw := body[urlStart:urlEnd]
		rewritten := rewriteURLFast(raw, schemeLen, baseURL)
		out = append(out, rewritten...)

		i = urlEnd
	}

	if out == nil {
		return body
	}
	return out
}

// rewriteURLFast rewrites a URL without url.Parse.
// Input: raw URL bytes like "https://example.com:8096/path?q=1"
// schemeLen: 7 for http://, 8 for https://
// Output: "baseURL/https/example.com/8096/path?q=1"
func rewriteURLFast(raw []byte, schemeLen int, baseURL string) []byte {
	// After scheme, find host[:port] boundary
	afterScheme := raw[schemeLen:]
	slashIdx := bytes.IndexByte(afterScheme, '/')
	var hostPort, pathAndQuery []byte
	if slashIdx >= 0 {
		hostPort = afterScheme[:slashIdx]
		pathAndQuery = afterScheme[slashIdx:] // includes leading /
	} else {
		hostPort = afterScheme
		pathAndQuery = []byte("/")
	}

	if len(hostPort) == 0 {
		return raw // malformed, return as-is
	}

	// Split host and port
	var host, portStr []byte
	// Handle IPv6: [::1]:port
	if hostPort[0] == '[' {
		bracketEnd := bytes.IndexByte(hostPort, ']')
		if bracketEnd < 0 {
			return raw
		}
		host = hostPort[1:bracketEnd]
		rest := hostPort[bracketEnd+1:]
		if len(rest) > 0 && rest[0] == ':' {
			portStr = rest[1:]
		}
	} else {
		colonIdx := bytes.LastIndexByte(hostPort, ':')
		if colonIdx >= 0 {
			host = hostPort[:colonIdx]
			portStr = hostPort[colonIdx+1:]
		} else {
			host = hostPort
		}
	}

	if len(host) == 0 {
		return raw
	}

	// Determine port number
	scheme := "http"
	if schemeLen == 8 {
		scheme = "https"
	}
	port := 80
	if scheme == "https" {
		port = 443
	}
	if len(portStr) > 0 {
		if p, err := strconv.Atoi(string(portStr)); err == nil && p > 0 && p <= 65535 {
			port = p
		}
	}

	// Build: baseURL + "/" + scheme + "/" + host + "/" + port + pathAndQuery
	var b strings.Builder
	b.Grow(len(baseURL) + 1 + len(scheme) + 1 + len(host) + 1 + 5 + len(pathAndQuery))
	b.WriteString(baseURL)
	b.WriteByte('/')
	b.WriteString(scheme)
	b.WriteByte('/')
	b.Write(host)
	b.WriteByte('/')
	b.WriteString(strconv.Itoa(port))
	b.Write(pathAndQuery)
	return []byte(b.String())
}

// isURLTerminator returns true for characters that end a URL in JSON/HTML/XML context.
func isURLTerminator(c byte) bool {
	switch c {
	case '"', '\'', '<', '>', ' ', '\t', '\n', '\r', '`', '(', ')', '{', '}', '[', ']', '\\', '|', '^':
		return true
	}
	return false
}

// rewriteSingleURL is used only for Location/Content-Location headers (few calls).
func rewriteSingleURL(rawURL, baseURL string) string {
	var schemeLen int
	if strings.HasPrefix(rawURL, "https://") {
		schemeLen = 8
	} else if strings.HasPrefix(rawURL, "http://") {
		schemeLen = 7
	} else {
		return rawURL
	}
	result := rewriteURLFast([]byte(rawURL), schemeLen, baseURL)
	return string(result)
}
