package server

import (
	"net/http"
)

// newOriginGuard admits only Host headers that name this installation, then rejects cross-origin browser writes and header-less writes to browser-only paths (ADR-0052).
func newOriginGuard(next http.Handler, hosts HostPolicy, browserOriginRequiredPaths []string) (http.Handler, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range hosts.TrustedOrigins() {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
	}
	requiredPaths := make(map[string]bool, len(browserOriginRequiredPaths))
	for _, requiredPath := range browserOriginRequiredPaths {
		requiredPaths[requiredPath] = true
	}
	guarded := protection.Handler(requireBrowserProvenance(next, requiredPaths))
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !hosts.AllowsHost(request.Host) {
			http.Error(writer, "unrecognized host", http.StatusMisdirectedRequest)
			return
		}
		guarded.ServeHTTP(writer, request)
	}), nil
}

// requireBrowserProvenance refuses unsafe requests to the listed paths unless they carry Origin or Sec-Fetch-Site; CrossOriginProtection has already verified whichever is present.
func requireBrowserProvenance(next http.Handler, requiredPaths map[string]bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if requiredPaths[request.URL.Path] && !isSafeMethod(request.Method) &&
			request.Header.Get("Origin") == "" && request.Header.Get("Sec-Fetch-Site") == "" {
			http.Error(writer, "origin required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

// isSafeMethod reports whether an HTTP method is defined as read-only by RFC 9110 and exempt from origin checks.
func isSafeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
