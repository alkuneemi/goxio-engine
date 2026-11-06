package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/portal"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func TestSignupEmptyTenantAndLoginAliases(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	dir := t.TempDir()
	p, err := portal.Open(dir, portal.Options{
		BcryptCost: bcrypt.MinCost, SecureCookies: false, LoginMax: 8, LoginWindow: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	if _, err := p.CreateAccount("owner", "password-owner-ok"); err != nil {
		t.Fatal(err)
	}
	owner, err := p.Authenticate("owner", "password-owner-ok")
	if err != nil {
		t.Fatal(err)
	}
	srv, _, err := p.AddServer(owner.AccountID, "owner-box", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	snap := space.Snapshot{Host: "owner-host", Source: "ingest", Nodes: []nodes.Node{{
		Id: "space-volume-owner", Type: "volume", Name: "owner-only-marker", Health: "unknown",
		Metadata: map[string]any{"kind": "volume", "size_state": "measured"},
	}}}
	if err := p.SaveSnapshot(owner.AccountID, srv.ID, snap); err != nil {
		t.Fatal(err)
	}

	h := s.RegisterRoutes()
	post := func(path, body, origin string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GoXIO-Request", "1")
		req.RemoteAddr = "198.51.100.77:77"
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	forged := `{"login":"fresh@example.com","password":"password-fresh-ok","confirm":"password-fresh-ok","account_id":"` + owner.AccountID + `","tenant_id":"owner","server_id":"` + srv.ID + `"}`
	if w := post("/api/portal/register", forged, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("forged register %d %s", w.Code, w.Body.String())
	}
	if _, err := p.FindAccount("fresh@example.com"); err != portal.ErrNotFound {
		t.Fatal("forged tenant id created an account")
	}

	if w := post("/api/portal/register", `{"login":"fresh@example.com","password":"password-fresh-ok","confirm":"other-password-ok"}`, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("mismatch %d %s", w.Code, w.Body.String())
	}
	if w := post("/api/portal/register", `{"login":"fresh@example.com","password":"short","confirm":"short"}`, ""); w.Code != http.StatusBadRequest {
		t.Fatalf("short %d %s", w.Code, w.Body.String())
	}
	if _, err := p.FindAccount("fresh@example.com"); err != portal.ErrNotFound {
		t.Fatal("rejected signup created an account")
	}
	if w := post("/api/portal/register", `{"login":"fresh@example.com","password":"password-fresh-ok","confirm":"password-fresh-ok"}`, "https://evil.example"); w.Code != http.StatusBadRequest {
		t.Fatalf("cross origin %d %s", w.Code, w.Body.String())
	}
	if w := post("/api/portal/register", `{"login":"owner@example.com","password":"another-password-ok","confirm":"another-password-ok"}`, "http://example.com"); w.Code != http.StatusConflict {
		t.Fatalf("slug email signup %d %s", w.Code, w.Body.String())
	}
	if _, err := p.FindAccount("owner@example.com"); err != portal.ErrNotFound {
		t.Fatal("signup attached itself to the owner")
	}

	w := post("/api/portal/register", `{"login":"Fresh@Example.com","password":"password-fresh-ok\n","confirm":" password-fresh-ok "}`, "http://example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("register %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			Status  string           `json:"status"`
			CSRF    string           `json:"csrf"`
			Servers []map[string]any `json:"servers"`
			Detail  string           `json:"detail"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Data.Status != "empty" || len(created.Data.Servers) != 0 || !strings.Contains(created.Data.Detail, "empty") {
		t.Fatalf("created = %+v", created.Data)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "goxio_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure {
		t.Fatalf("cookie = %#v", cookie)
	}

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		authed(req, cookie, "")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if servers := get("/api/portal/servers"); servers.Code != http.StatusOK || strings.Contains(servers.Body.String(), "owner-box") || !strings.Contains(servers.Body.String(), "[]") {
		t.Fatalf("servers %d %s", servers.Code, servers.Body.String())
	}
	graph := get("/api/graph?server=" + srv.ID)
	if graph.Code != http.StatusNotFound || strings.Contains(graph.Body.String(), "owner-only-marker") {
		t.Fatalf("forged graph %d %s", graph.Code, graph.Body.String())
	}
	ws := get("/websocket?server=" + srv.ID)
	if ws.Code == http.StatusOK || strings.Contains(ws.Body.String(), "owner-only-marker") {
		t.Fatalf("websocket %d %s", ws.Code, ws.Body.String())
	}
	ing := httptest.NewRequest(http.MethodPost, "/api/space/ingest", strings.NewReader(`{"host":"x","nodes":[]}`))
	authed(ing, cookie, created.Data.CSRF)
	iw := httptest.NewRecorder()
	h.ServeHTTP(iw, ing)
	if iw.Code == http.StatusOK {
		t.Fatalf("signup session ingested: %d %s", iw.Code, iw.Body.String())
	}

	again := post("/api/portal/register", `{"login":"fresh@example.com","password":"different-password","confirm":"different-password"}`, "http://example.com")
	if again.Code != http.StatusConflict {
		t.Fatalf("duplicate %d %s", again.Code, again.Body.String())
	}
	if _, err := p.Authenticate("fresh@example.com", "different-password"); err != portal.ErrInvalid {
		t.Fatal("duplicate password replaced the account")
	}

	pasted := post("/api/portal/login", `{"login":"OWNER","password":"password-owner-ok\r\n"}`, "http://example.com")
	if pasted.Code != http.StatusOK {
		t.Fatalf("pasted owner login %d %s", pasted.Code, pasted.Body.String())
	}
	emailed := post("/api/portal/login", `{"login":"Owner@Example.com","password":"password-owner-ok"}`, "")
	if emailed.Code != http.StatusOK {
		t.Fatalf("owner email login %d %s", emailed.Code, emailed.Body.String())
	}
	bad := post("/api/portal/login", `{"login":"owner","password":"not-the-password"}`, "")
	if bad.Code != http.StatusUnauthorized || !strings.Contains(bad.Body.String(), "invalid login or password") || !strings.Contains(bad.Body.String(), "لم تُفتح جلسة") {
		t.Fatalf("bad login %d %s", bad.Code, bad.Body.String())
	}
	for _, c := range bad.Result().Cookies() {
		if c.Name == "goxio_session" && c.Value != "" && c.MaxAge >= 0 {
			t.Fatalf("failed login set a session: %#v", c)
		}
	}

	p2, err := portal.Open(dir, portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p2.LookupSession(cookie.Value); err != nil {
		t.Fatalf("session did not survive restart: %v", err)
	}
	fresh, err := p2.FindAccount("fresh@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID == owner.AccountID {
		t.Fatal("signup reused the owner id")
	}
	list, err := p2.ListServers(fresh.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("servers after restart = %+v %v", list, err)
	}
	if _, err := p2.Authenticate("FRESH@example.com", "password-fresh-ok"); err != nil {
		t.Fatal(err)
	}
	ownerGraph := p2
	_ = ownerGraph
	g, _, err := p2.Graph(owner.AccountID, srv.ID)
	if err != nil || !strings.Contains(mustJSON(g), "owner-only-marker") {
		t.Fatal("owner map was lost")
	}
	if _, _, err := p2.Graph(fresh.ID, srv.ID); err == nil {
		t.Fatal("fresh account read the owner server")
	}
}

func TestSecureRegisterCookie(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	req := httptest.NewRequest(http.MethodPost, "/api/portal/register", strings.NewReader(`{"login":"secure-user","password":"password-secure-ok","confirm":"password-secure-ok"}`))
	req.Header.Set("X-GoXIO-Request", "1")
	req.Host = "goxio.example"
	req.Header.Set("Origin", "https://goxio.example")
	req.RemoteAddr = "198.51.100.70:70"
	w := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "__Host-goxio_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("cookie = %#v", cookie)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSignupRateLimit(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false, LoginMax: 2, LoginWindow: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	var last int
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/portal/register", strings.NewReader(`{"login":"rate-user","password":"short","confirm":"short"}`))
		req.Header.Set("X-GoXIO-Request", "1")
		req.RemoteAddr = "198.51.100.71:71"
		w := httptest.NewRecorder()
		s.RegisterRoutes().ServeHTTP(w, req)
		last = w.Code
		io.Copy(io.Discard, w.Body)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("rate = %d", last)
	}
}
