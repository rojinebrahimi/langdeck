package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(ClientIP(r.Context()))) })

func TestRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")

	rec := httptest.NewRecorder()
	RealIP(false)(ok).ServeHTTP(rec, req)
	if rec.Body.String() != "10.0.0.1" {
		t.Fatalf("untrusted proxy headers must be ignored, got %s", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	RealIP(true)(ok).ServeHTTP(rec, req)
	if rec.Body.String() != "203.0.113.9" {
		t.Fatalf("trusted XFF must be used, got %s", rec.Body.String())
	}
}

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(0.0001, 2)
	first, second, third := l.Allow("a"), l.Allow("a"), l.Allow("a")
	if !first || !second || third {
		t.Fatal("burst of 2 then deny")
	}
	if !l.Allow("b") {
		t.Fatal("keys are independent")
	}
}

func TestCORS(t *testing.T) {
	h := CORS([]string{"https://ok.example"})(ok)
	req := httptest.NewRequest("OPTIONS", "/", nil)
	req.Header.Set("Origin", "https://ok.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "https://ok.example" {
		t.Fatalf("allowed origin preflight failed: %d", rec.Code)
	}
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unknown origins must get no CORS headers")
	}
}

func TestRequestIDSanitized(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-ID", "bad id\r\ninjected")
	rec := httptest.NewRecorder()
	WithRequestID(ok).ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got == "" || got == "bad id\r\ninjected" {
		t.Fatalf("request id must be replaced, got %q", got)
	}
}

func TestRecover(t *testing.T) {
	h := Recover(discard())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("want 500, got %d", rec.Code)
	}
}
