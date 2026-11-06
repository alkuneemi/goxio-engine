package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/guilherme-grimm/graph-go/internal/portal"
)

func TestSpoofedForwardedForDoesNotRefreshLoginLimit(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{
		BcryptCost: bcrypt.MinCost, SecureCookies: false, LoginMax: 2, LoginWindow: time.Minute,
		TrustProxy: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	if _, err := p.CreateAccount("limited", "password-limited-ok"); err != nil {
		t.Fatal(err)
	}
	h := s.RegisterRoutes()
	var last *httptest.ResponseRecorder
	for i, doc := range []string{"198.51.100.1", "198.51.100.2", "203.0.113.9"} {
		req := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"limited","password":"wrong-password-value"}`))
		req.Header.Set("X-GoXIO-Request", "1")
		req.Header.Set("X-Forwarded-For", doc)
		req.Header.Set("CF-Connecting-IP", doc)
		req.RemoteAddr = "203.0.113.50:4000"
		last = httptest.NewRecorder()
		h.ServeHTTP(last, req)
		if i < 2 && last.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d %s", i, last.Code, last.Body.String())
		}
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("forged forwarded addresses did not share a bucket: %d %s", last.Code, last.Body.String())
	}
	if last.Header().Get("Retry-After") == "" {
		t.Fatal("429 did not include Retry-After")
	}
}

func TestTrustedProxyKeepsClientsSeparate(t *testing.T) {
	trust, err := portal.ParseTrustedProxies("10.8.0.0/16")
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{
		BcryptCost: bcrypt.MinCost, SecureCookies: false, LoginMax: 1, LoginWindow: time.Minute,
		TrustProxy: true, TrustedProxies: trust,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	if _, err := p.CreateAccount("limited", "password-limited-ok"); err != nil {
		t.Fatal(err)
	}
	h := s.RegisterRoutes()
	post := func(xff string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"limited","password":"wrong-password-value"}`))
		req.Header.Set("X-GoXIO-Request", "1")
		req.Header.Set("X-Forwarded-For", "198.51.100.99, "+xff)
		req.RemoteAddr = "10.8.0.4:443"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if code := post("203.0.113.10"); code != http.StatusUnauthorized {
		t.Fatalf("first client = %d", code)
	}
	if code := post("203.0.113.10"); code != http.StatusTooManyRequests {
		t.Fatalf("same client second try = %d", code)
	}
	if code := post("203.0.113.11"); code != http.StatusUnauthorized {
		t.Fatalf("a different client behind the proxy was blocked: %d", code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"limited","password":"wrong-password-value"}`))
	req.Header.Set("X-GoXIO-Request", "1")
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 203.0.113.10")
	req.RemoteAddr = "10.8.0.4:443"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("left-hand documentation address opened a new bucket: %d", w.Code)
	}
}
