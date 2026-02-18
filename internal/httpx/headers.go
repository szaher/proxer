package httpx

import (
	"net/http"
	"strings"
)

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"proxy-connection":    {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
}

func IsHopByHopHeader(key string) bool {
	_, ok := hopByHopHeaders[strings.ToLower(key)]
	return ok
}

func CloneHTTPHeader(src http.Header) map[string][]string {
	dst := make(map[string][]string, len(src))
	for k, values := range src {
		if IsHopByHopHeader(k) {
			continue
		}
		cloned := make([]string, 0, len(values))
		for _, value := range values {
			cloned = append(cloned, value)
		}
		dst[k] = cloned
	}
	return dst
}

func CloneMapHeader(src map[string][]string) map[string][]string {
	dst := make(map[string][]string, len(src))
	for k, values := range src {
		if IsHopByHopHeader(k) {
			continue
		}
		cloned := make([]string, 0, len(values))
		for _, value := range values {
			cloned = append(cloned, value)
		}
		dst[k] = cloned
	}
	return dst
}

func WriteHeaderMap(dst http.Header, src map[string][]string) {
	for k, values := range src {
		if IsHopByHopHeader(k) {
			continue
		}
		for _, value := range values {
			dst.Add(k, value)
		}
	}
}
