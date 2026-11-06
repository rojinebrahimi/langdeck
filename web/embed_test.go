package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func serve(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestSPAHandler(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/app-1.js": {Data: []byte("console.log(1)")},
		"logo.svg":        {Data: []byte("<svg/>")},
	})

	if rec := serve(h, "/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") || rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index: %d %q %q", rec.Code, rec.Body, rec.Header().Get("Cache-Control"))
	}
	rec := serve(h, "/agents/123")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("client-side routes must fall back to index.html, got %d", rec.Code)
	}
	if rec := serve(h, "/assets/app-1.js"); !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("hashed assets must be cached long-term, got %q", rec.Header().Get("Cache-Control"))
	}
	if rec := serve(h, "/api/v1/unknown"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown API paths must 404, not return the SPA: %d", rec.Code)
	}
	if rec := serve(h, "/../../etc/passwd"); strings.Contains(rec.Body.String(), "root:") {
		t.Fatal("path traversal must not escape the embedded files")
	}
}

func TestHandlerWithoutBuiltUI(t *testing.T) {
	rec := serve(handler(fstest.MapFS{".keep": {}}), "/")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "make ui") {
		t.Fatalf("a missing UI build must explain how to fix it: %d %q", rec.Code, rec.Body)
	}
}
