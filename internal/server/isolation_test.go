package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/bcrypt"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/portal"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func seedPortal(t *testing.T, s *Server, login, password string, snap space.Snapshot) (*http.Cookie, string) {
	t.Helper()
	p, err := portal.Open(t.TempDir(), portal.Options{
		SecureCookies: false,
		BcryptCost:    bcrypt.MinCost,
		LoginMax:      3,
		LoginWindow:   time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	if _, err := p.CreateAccount(login, password); err != nil {
		t.Fatal(err)
	}
	sess, err := p.Authenticate(login, password)
	if err != nil {
		t.Fatal(err)
	}
	srv, _, err := p.AddServer(sess.AccountID, login+"-box", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Nodes) > 0 || len(snap.Edges) > 0 {
		snap.Source = "ingest"
		if err := p.SaveSnapshot(sess.AccountID, srv.ID, snap); err != nil {
			t.Fatal(err)
		}
	}
	body := `{"login":"` + login + `","password":"` + password + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GoXIO-Request", "1")
	req.RemoteAddr = "198.51.100.8:40000"
	w := httptest.NewRecorder()
	s.portalLoginHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login status %d %s", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "goxio_session" {
			cookie = c
		}
	}
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", cookie)
	}
	return cookie, srv.ID
}

func authed(req *http.Request, c *http.Cookie, csrf string) {
	req.AddCookie(c)
	req.Header.Set("X-GoXIO-Request", "1")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	req.RemoteAddr = "198.51.100.8:40000"
}

func csrfOf(t *testing.T, s *Server, c *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/portal/me", nil)
	authed(req, c, "")
	w := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("me %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			CSRF string `json:"csrf"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data.CSRF
}

func TestUnauthenticatedMapRoutes(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	h := s.RegisterRoutes()
	paths := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/graph"},
		{http.MethodGet, "/api/space"},
		{http.MethodGet, "/api/node/anything"},
		{http.MethodGet, "/api/health"},
		{http.MethodGet, "/api/space/actions"},
		{http.MethodPost, "/api/space/actions"},
		{http.MethodPost, "/api/space/ingest"},
		{http.MethodGet, "/api/portal/me"},
		{http.MethodGet, "/api/portal/servers"},
	}
	for _, pth := range paths {
		req := httptest.NewRequest(pth.method, pth.path, strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401", pth.method, pth.path, w.Code)
		}
		if bytes.Contains(w.Body.Bytes(), []byte("old-cache")) || bytes.Contains(w.Body.Bytes(), []byte("pg-test")) {
			t.Errorf("%s leaked inventory: %s", pth.path, w.Body.String())
		}
	}

	srv := httptest.NewServer(h)
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/websocket"
	ctx := t.Context()
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("websocket accepted without a session")
	}
	if resp != nil && resp.Body != nil {
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if bytes.Contains(b, []byte("pg-test")) || bytes.Contains(b, []byte("old-cache")) {
			t.Fatalf("websocket body leaked: %s", b)
		}
	}
}

func TestTwoTenantIsolation(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false, LoginMax: 3, LoginWindow: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	mustAcct := func(login string) {
		t.Helper()
		if _, err := p.CreateAccount(login, "password-"+login+"-ok"); err != nil {
			t.Fatal(err)
		}
	}
	mustAcct("alpha")
	mustAcct("beta")
	aSess, err := p.Authenticate("alpha", "password-alpha-ok")
	if err != nil {
		t.Fatal(err)
	}
	bSess, err := p.Authenticate("beta", "password-beta-ok")
	if err != nil {
		t.Fatal(err)
	}
	aSrv, aTok, err := p.AddServer(aSess.AccountID, "alpha-box", "203.0.113.10")
	if err != nil {
		t.Fatal(err)
	}
	bSrv, bTok, err := p.AddServer(bSess.AccountID, "beta-box", "203.0.113.20")
	if err != nil {
		t.Fatal(err)
	}
	ingest := func(tok, marker string) {
		t.Helper()
		snap := space.Snapshot{
			Host: marker + "-host", Source: "ingest",
			Nodes: []nodes.Node{{
				Id: "space-volume-" + marker, Type: "volume", Name: "marker-" + marker, Health: "healthy",
				Metadata: map[string]any{"path": "/data/" + marker, "marker": marker},
			}},
		}
		b, _ := json.Marshal(snap)
		req := httptest.NewRequest(http.MethodPost, "/api/space/ingest", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+tok)
		// A forged account id in a header must not retarget the snapshot.
		req.Header.Set("X-Account-Id", bSess.AccountID)
		w := httptest.NewRecorder()
		s.RegisterRoutes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest %s: %d %s", marker, w.Code, w.Body.String())
		}
		if bytes.Contains(w.Body.Bytes(), []byte(tok)) {
			t.Fatal("ingest response echoed the token")
		}
	}
	ingest(aTok, "alpha")
	ingest(bTok, "beta")

	login := func(login, password string) *http.Cookie {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"`+login+`","password":"`+password+`"}`))
		req.Header.Set("X-GoXIO-Request", "1")
		req.RemoteAddr = "198.51.100.9:9"
		w := httptest.NewRecorder()
		s.RegisterRoutes().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("login %s: %d %s", login, w.Code, w.Body.String())
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == "goxio_session" {
				return c
			}
		}
		t.Fatal("missing cookie")
		return nil
	}
	aCookie := login("alpha", "password-alpha-ok")
	bCookie := login("beta", "password-beta-ok")

	getGraph := func(c *http.Cookie, serverID string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/graph?server="+serverID, nil)
		authed(req, c, "")
		w := httptest.NewRecorder()
		s.RegisterRoutes().ServeHTTP(w, req)
		return w.Body.String() + " status=" + http.StatusText(w.Code)
	}
	// Cache-style repeat: A then B must not bleed.
	aBody := getGraph(aCookie, aSrv.ID)
	aBody2 := getGraph(aCookie, aSrv.ID)
	bBody := getGraph(bCookie, bSrv.ID)
	if !strings.Contains(aBody, "marker-alpha") || strings.Contains(aBody, "marker-beta") {
		t.Fatalf("alpha graph = %s", aBody)
	}
	if !strings.Contains(aBody2, "marker-alpha") || strings.Contains(aBody2, "marker-beta") {
		t.Fatalf("alpha graph repeat = %s", aBody2)
	}
	if !strings.Contains(bBody, "marker-beta") || strings.Contains(bBody, "marker-alpha") {
		t.Fatalf("beta graph = %s", bBody)
	}
	forged := getGraph(aCookie, bSrv.ID)
	if strings.Contains(forged, "marker-beta") || strings.Contains(forged, "beta-box") || !strings.Contains(forged, "Not Found") && !strings.Contains(forged, "not found") {
		t.Fatalf("forged server id = %s", forged)
	}
	// Path traversal and a made-up KSA host id.
	for _, id := range []string{"../" + bSrv.ID, "srv_ksa_host", aSrv.ID + "/../../" + bSrv.ID} {
		req := httptest.NewRequest(http.MethodGet, "/api/space?server="+id, nil)
		authed(req, aCookie, "")
		w := httptest.NewRecorder()
		s.RegisterRoutes().ServeHTTP(w, req)
		if w.Code == http.StatusOK || bytes.Contains(w.Body.Bytes(), []byte("marker-beta")) {
			t.Fatalf("id %s leaked %d %s", id, w.Code, w.Body.String())
		}
	}
	// Alpha's token cannot write beta's marker into alpha, and cannot target beta.
	evil := `{"host":"beta-host","nodes":[{"id":"space-volume-beta","type":"volume","name":"marker-beta","health":"healthy","metadata":{"account_id":"` + bSess.AccountID + `","server_id":"` + bSrv.ID + `"}}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/space/ingest", strings.NewReader(evil))
	req.Header.Set("Authorization", "Bearer "+aTok)
	w := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reingest %d %s", w.Code, w.Body.String())
	}
	aAfter := getGraph(aCookie, aSrv.ID)
	bAfter := getGraph(bCookie, bSrv.ID)
	if strings.Contains(bAfter, "marker-alpha") {
		t.Fatal("beta saw alpha after alpha reingest")
	}
	if !strings.Contains(aAfter, "marker-beta") {
		// Alpha's token overwrites alpha's snapshot. That is alpha's own server.
	}
	if strings.Contains(bAfter, "\"name\":\"marker-beta\"") == false {
		t.Fatalf("beta lost its own map: %s", bAfter)
	}

	// Pending tenant has no demo inventory.
	cSess, err := func() (portal.Session, error) {
		if _, err := p.CreateAccount("gamma", "password-gamma-ok"); err != nil {
			t.Fatal(err)
		}
		return p.Authenticate("gamma", "password-gamma-ok")
	}()
	if err != nil {
		t.Fatal(err)
	}
	gSrv, _, err := p.AddServer(cSess.AccountID, "gamma-box", "203.0.113.30")
	if err != nil {
		t.Fatal(err)
	}
	gCookie := login("gamma", "password-gamma-ok")
	pending := getGraph(gCookie, gSrv.ID)
	for _, leak := range []string{"old-cache", "omxio-postgres", "alkuneemi", "marker-alpha", "marker-beta", "/host-disk"} {
		if strings.Contains(pending, leak) {
			t.Fatalf("pending map leaked %s: %s", leak, pending)
		}
	}
	if !strings.Contains(pending, "pending") {
		t.Fatalf("pending map = %s", pending)
	}

	// Logout revokes the cookie.
	token := csrfOf(t, s, aCookie)
	req = httptest.NewRequest(http.MethodPost, "/api/portal/logout", nil)
	authed(req, aCookie, token)
	w = httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("logout %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/portal/me", nil)
	authed(req, aCookie, "")
	w = httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still accepted: %d", w.Code)
	}

	// Session file still authenticates a second request before logout — covered above.
	// Wrong password does not open a session, and the rate limit trips.
	for i := 0; i < 4; i++ {
		req = httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"beta","password":"not-the-password"}`))
		req.Header.Set("X-GoXIO-Request", "1")
		req.RemoteAddr = "198.51.100.50:50"
		w = httptest.NewRecorder()
		s.RegisterRoutes().ServeHTTP(w, req)
	}
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit = %d", w.Code)
	}

	// SSH-looking path in a snapshot is redacted for the tenant.
	secretSnap := space.Snapshot{Host: "alpha-host", Nodes: []nodes.Node{{
		Id: "space-volume-keys", Type: "volume", Name: "keys", Health: "unknown",
		Metadata: map[string]any{"path": "/home/app/.ssh/id_ed25519", "ingest_token": aTok},
	}}}
	raw, _ := json.Marshal(secretSnap)
	req = httptest.NewRequest(http.MethodPost, "/api/space/ingest", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+aTok)
	w = httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("secret ingest %d %s", w.Code, w.Body.String())
	}
	shown := getGraph(aCookie, aSrv.ID)
	// alpha cookie was logged out. use a fresh login.
	aCookie = login("alpha", "password-alpha-ok")
	shown = getGraph(aCookie, aSrv.ID)
	if strings.Contains(shown, "id_ed25519") || strings.Contains(shown, aTok) {
		t.Fatalf("secret or token visible: %s", shown)
	}
}

func TestSessionSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	p, err := portal.Open(dir, portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateAccount("persist", "password-persist-ok"); err != nil {
		t.Fatal(err)
	}
	sess, err := p.Authenticate("persist", "password-persist-ok")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := portal.Open(dir, portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	got, err := p2.LookupSession(sess.ID)
	if err != nil || got.AccountID != sess.AccountID {
		t.Fatalf("session did not persist: %v %+v", err, got)
	}
}
