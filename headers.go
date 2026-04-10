package main

import (
	"net/http"
	"strings"
)

var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

var stripRequestHeaders = []string{
	"X-Real-Ip",
	"X-Forwarded-For",
	"X-Forwarded-Proto",
	"X-Forwarded-Host",
	"X-Forwarded-Port",
	"X-Forwarded-Prefix",
	"Forwarded",
	"Via",
}

var stripResponseHeaders = []string{
	"Server",
	"X-Powered-By",
}

var safeContentEncodings = map[string]bool{
	"":         true,
	"identity": true,
}

func copyRequestHeaders(dst, src http.Header, allowUpgrade bool) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hopByHopHeaders[ck] && (!allowUpgrade || (ck != "Connection" && ck != "Upgrade")) {
			continue
		}
		dst[ck] = append([]string(nil), vs...)
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, vs := range src {
		if hopByHopHeaders[k] {
			continue
		}
		dst[k] = vs
	}
	for _, name := range stripResponseHeaders {
		dst.Del(name)
	}
}

func rewriteProxySensitiveRequestHeaders(h http.Header, forwardedPrefix string) {
	for _, name := range stripRequestHeaders {
		h.Del(name)
	}
	if ref := h.Get("Referer"); ref != "" {
		h.Set("Referer", unproxyURL(ref, forwardedPrefix))
	}
	if origin := h.Get("Origin"); origin != "" {
		h.Set("Origin", unproxyURL(origin, forwardedPrefix))
	}
}

func setUpstreamHost(req *http.Request, t *target) {
	hostVal := targetHostPort(t)
	req.Host = hostVal
	req.Header.Set("Host", hostVal)
}

func headerContainsToken(h http.Header, key, token string) bool {
	for _, part := range strings.Split(h.Get(key), ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func normalizeContentEncoding(raw string) string {
	if raw == "" {
		return ""
	}
	if idx := strings.IndexByte(raw, ','); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(strings.ToLower(raw))
}
