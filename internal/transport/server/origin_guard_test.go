package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aportcullis/portcullis/internal/platform/publicorigin"
	"github.com/aportcullis/portcullis/internal/transport/server"
)

const (
	credentialPath    = "/portcullis.v1.Auth/Bootstrap"
	authenticatedPath = "/portcullis.v1.Auth/Me"
	publishedOrigin   = "https://portcullis.example.com"
)

func loopbackHostPolicy(t *testing.T) publicorigin.Policy {
	t.Helper()
	policy, err := publicorigin.ParsePolicy(nil)
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	return policy
}

// newGuardedServer serves a stub Auth mount that answers 204, so a 204 proves the request crossed the origin boundary.
func newGuardedServer(t *testing.T, origins ...string) *httptest.Server {
	t.Helper()
	policy, err := publicorigin.ParsePolicy(origins)
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	reached := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { writer.WriteHeader(http.StatusNoContent) })
	srv, err := server.New(server.Options{
		Addr:                       ":0",
		Logger:                     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hosts:                      policy,
		BrowserOriginRequiredPaths: []string{credentialPath},
	}, server.Mount{Pattern: "/portcullis.v1.Auth/", Handler: reached})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	testServer := httptest.NewServer(srv.Handler())
	t.Cleanup(testServer.Close)
	return testServer
}

type guardedRequest struct {
	name, method, path, host string
	headers                  map[string]string
	wantStatus               int
}

func sendGuardedRequest(t *testing.T, testServer *httptest.Server, request guardedRequest) {
	t.Helper()
	var body io.Reader
	if request.method == http.MethodPost {
		body = strings.NewReader("{}")
	}
	httpRequest, err := http.NewRequest(request.method, testServer.URL+request.path, body)
	if err != nil {
		t.Fatalf("%s: NewRequest: %v", request.name, err)
	}
	httpRequest.Host = request.host
	httpRequest.Header.Set("Content-Type", "application/json")
	for name, value := range request.headers {
		httpRequest.Header.Set(name, value)
	}
	response, err := testServer.Client().Do(httpRequest)
	if err != nil {
		t.Fatalf("%s: %v", request.name, err)
	}
	_ = response.Body.Close()
	if response.StatusCode != request.wantStatus {
		t.Errorf("%s: status = %d, want %d", request.name, response.StatusCode, request.wantStatus)
	}
}

func TestOriginGuardAdmitsPublishedHostsAndSameOriginRequests(t *testing.T) {
	t.Parallel()
	published := newGuardedServer(t, publishedOrigin)
	for _, request := range []guardedRequest{
		{name: "SPA on the published host", method: http.MethodGet, path: "/", host: "portcullis.example.com", wantStatus: http.StatusOK},
		{name: "explicit default port", method: http.MethodGet, path: "/connections", host: "portcullis.example.com:443", wantStatus: http.StatusOK},
		{name: "same-origin credential POST by Origin", method: http.MethodPost, path: credentialPath, host: "portcullis.example.com", headers: map[string]string{"Origin": publishedOrigin}, wantStatus: http.StatusNoContent},
		{name: "same-origin credential POST by Sec-Fetch-Site", method: http.MethodPost, path: credentialPath, host: "portcullis.example.com", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, wantStatus: http.StatusNoContent},
		{name: "non-browser authenticated RPC without Origin", method: http.MethodPost, path: authenticatedPath, host: "portcullis.example.com", wantStatus: http.StatusNoContent},
		{name: "probe with a pod IP Host", method: http.MethodGet, path: "/readyz", host: "10.42.0.17:8080", wantStatus: http.StatusOK},
	} {
		sendGuardedRequest(t, published, request)
	}

	loopback := newGuardedServer(t)
	for _, request := range []guardedRequest{
		{name: "localhost demo", method: http.MethodGet, path: "/", host: "localhost:8080", wantStatus: http.StatusOK},
		{name: "IPv4 loopback credential POST", method: http.MethodPost, path: credentialPath, host: "127.0.0.1:18080", headers: map[string]string{"Origin": "http://127.0.0.1:18080"}, wantStatus: http.StatusNoContent},
		{name: "IPv6 loopback", method: http.MethodGet, path: "/livez", host: "[::1]:8080", wantStatus: http.StatusOK},
	} {
		sendGuardedRequest(t, loopback, request)
	}
}

func TestOriginGuardRefusesRebindingAndCrossOriginRequests(t *testing.T) {
	t.Parallel()
	published := newGuardedServer(t, publishedOrigin)
	for _, request := range []guardedRequest{
		{name: "rebound attacker Host on the SPA", method: http.MethodGet, path: "/", host: "attacker.example", wantStatus: http.StatusMisdirectedRequest},
		{name: "rebound attacker Host on a credential RPC with its own Origin", method: http.MethodPost, path: credentialPath, host: "attacker.example", headers: map[string]string{"Origin": "http://attacker.example"}, wantStatus: http.StatusMisdirectedRequest},
		{name: "published name on another port", method: http.MethodPost, path: credentialPath, host: "portcullis.example.com:8443", headers: map[string]string{"Origin": "https://portcullis.example.com:8443"}, wantStatus: http.StatusMisdirectedRequest},
		{name: "foreign Origin on a credential RPC", method: http.MethodPost, path: credentialPath, host: "portcullis.example.com", headers: map[string]string{"Origin": "https://attacker.example"}, wantStatus: http.StatusForbidden},
		{name: "opaque null Origin", method: http.MethodPost, path: credentialPath, host: "portcullis.example.com", headers: map[string]string{"Origin": "null"}, wantStatus: http.StatusForbidden},
		{name: "cross-site fetch metadata on an authenticated RPC", method: http.MethodPost, path: authenticatedPath, host: "portcullis.example.com", headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://attacker.example"}, wantStatus: http.StatusForbidden},
		{name: "credential RPC without browser provenance", method: http.MethodPost, path: credentialPath, host: "portcullis.example.com", wantStatus: http.StatusForbidden},
	} {
		sendGuardedRequest(t, published, request)
	}

	loopback := newGuardedServer(t)
	for _, request := range []guardedRequest{
		{name: "unset origins refuse a LAN name", method: http.MethodGet, path: "/", host: "portcullis.lan:8080", wantStatus: http.StatusMisdirectedRequest},
		{name: "unset origins refuse a LAN address", method: http.MethodPost, path: credentialPath, host: "192.168.1.20:8080", headers: map[string]string{"Origin": "http://192.168.1.20:8080"}, wantStatus: http.StatusMisdirectedRequest},
		{name: "another loopback port is cross-origin", method: http.MethodPost, path: credentialPath, host: "127.0.0.1:18080", headers: map[string]string{"Origin": "http://127.0.0.1:3000"}, wantStatus: http.StatusForbidden},
	} {
		sendGuardedRequest(t, loopback, request)
	}
}
