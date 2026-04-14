package main

import (
	"net/http"
	"net/url"
	"strings"
)

// clientMutationGuardMiddleware protects unsafe HTTP methods from cross-site
// form submissions while preserving JSON API compatibility for CLI callers.
func clientMutationGuardMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isUnsafeHTTPMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/api/") {
				if !isJSONContentType(r.Header.Get("Content-Type")) {
					writeJSONResponseWithStatus(w, http.StatusUnsupportedMediaType, map[string]string{
						"error": "application/json required",
					})
					return
				}
				if hasOriginOrReferer(r) && !requestIsSameOrigin(r) {
					writeJSONResponseWithStatus(w, http.StatusForbidden, map[string]string{
						"error": "forbidden origin",
					})
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if !requestIsSameOrigin(r) {
				http.Error(w, "Forbidden: no valid origin", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isUnsafeHTTPMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isJSONContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return false
	}
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct == "application/json"
}

func hasOriginOrReferer(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get("Origin")) != "" || strings.TrimSpace(r.Header.Get("Referer")) != ""
}

func requestIsSameOrigin(r *http.Request) bool {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xfp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xfp != "" {
		if idx := strings.Index(xfp, ","); idx >= 0 {
			xfp = xfp[:idx]
		}
		scheme = strings.TrimSpace(xfp)
	}

	host := strings.TrimSpace(r.Host)
	if xfh := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xfh != "" {
		if idx := strings.Index(xfh, ","); idx >= 0 {
			xfh = xfh[:idx]
		}
		host = strings.TrimSpace(xfh)
	}
	if host == "" {
		return false
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		return originMatchesRequest(origin, scheme, host)
	}

	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if referer == "" {
		return false
	}
	refURL, err := url.Parse(referer)
	if err != nil || refURL.Scheme == "" || refURL.Host == "" {
		return false
	}
	return originMatchesRequest(refURL.Scheme+"://"+refURL.Host, scheme, host)
}

func originMatchesRequest(rawOrigin, scheme, host string) bool {
	originURL, err := url.Parse(strings.TrimSpace(rawOrigin))
	if err != nil || originURL.Scheme == "" || originURL.Host == "" {
		return false
	}
	return strings.EqualFold(originURL.Scheme, scheme) && strings.EqualFold(originURL.Host, host)
}

