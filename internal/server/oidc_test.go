package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/portal"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

type fakeIDP struct {
	srv      *httptest.Server
	key      *rsa.PrivateKey
	kid      string
	clientID string
	secret   string
	redirect string

	mu         sync.Mutex
	codes      map[string]fakeCode
	tokenHits  int
	foreignHit *int
}

type fakeCode struct {
	challenge string
	claims    map[string]any
	used      bool
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIDP{
		key: key, kid: "test-key", clientID: "goxio-client", secret: "oidc-secret-value",
		redirect: "http://127.0.0.1/api/portal/oidc/callback",
		codes:    map[string]fakeCode{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("/jwks", f.jwks)
	mux.HandleFunc("/token", f.token)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIDP) discovery(w http.ResponseWriter, r *http.Request) {
	token := f.srv.URL + "/token"
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":                 f.srv.URL,
		"authorization_endpoint": f.srv.URL + "/authorize",
		"token_endpoint":         token,
		"jwks_uri":               f.srv.URL + "/jwks",
	})
}

func (f *fakeIDP) jwks(w http.ResponseWriter, r *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "kid": f.kid, "alg": "RS256", "use": "sig",
		"n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(bigEndian(f.key.E)),
	}}})
}

func bigEndian(e int) []byte {
	if e == 0 {
		return []byte{0}
	}
	var b []byte
	for e > 0 {
		b = append([]byte{byte(e)}, b...)
		e >>= 8
	}
	return b
}

func (f *fakeIDP) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.tokenHits++
	f.mu.Unlock()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad", http.StatusBadRequest)
		return
	}
	user, pass, ok := r.BasicAuth()
	if !ok || user != f.clientID || pass != f.secret || r.Form.Get("redirect_uri") != f.redirect {
		http.Error(w, "auth", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	code := f.codes[r.Form.Get("code")]
	if code.used || code.claims == nil {
		f.mu.Unlock()
		http.Error(w, "code", http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != code.challenge {
		f.mu.Unlock()
		http.Error(w, "pkce", http.StatusBadRequest)
		return
	}
	code.used = true
	f.codes[r.Form.Get("code")] = code
	f.mu.Unlock()
	raw := signRS256(f.key, f.kid, code.claims)
	_ = json.NewEncoder(w).Encode(map[string]string{"id_token": raw, "token_type": "Bearer", "access_token": "should-not-leak"})
}

func (f *fakeIDP) addCode(code, challenge string, claims map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code] = fakeCode{challenge: challenge, claims: claims}
}

func signRS256(key *rsa.PrivateKey, kid string, claims map[string]any) string {
	hb, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid})
	pb, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(hb)
	p := base64.RawURLEncoding.EncodeToString(pb)
	sum := sha256.Sum256([]byte(h + "." + p))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return h + "." + p + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fakeIDP) wire(s *Server) {
	s.oidc = newOIDC(oidcConfig{
		Issuer: f.srv.URL, ClientID: f.clientID, ClientSecret: f.secret, RedirectURI: f.redirect,
	})
}

func TestOIDCUnconfiguredStaysLocal(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	req := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/start", nil)
	w := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/?sso_error=unconfigured" {
		t.Fatalf("start %d %s", w.Code, w.Header().Get("Location"))
	}
	info := httptest.NewRequest(http.MethodGet, "/api/portal/oidc", nil)
	iw := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(iw, info)
	if !strings.Contains(iw.Body.String(), `"configured":false`) {
		t.Fatalf("info %s", iw.Body.String())
	}
}

func TestOIDCValidCodeIsEmptyTenantAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(dir, portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	idp := newFakeIDP(t)
	idp.wire(s)
	if _, err := p.CreateAccount("owner@example.com", "password-owner-ok"); err != nil {
		t.Fatal(err)
	}
	owner, err := p.Authenticate("owner@example.com", "password-owner-ok")
	if err != nil {
		t.Fatal(err)
	}
	box, _, err := p.AddServer(owner.AccountID, "owner-box", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SaveSnapshot(owner.AccountID, box.ID, space.Snapshot{Host: "owner", Source: "ingest", Nodes: []nodes.Node{{
		Id: "space-volume-owner", Type: "volume", Name: "owner-only-marker", Health: "unknown",
	}}}); err != nil {
		t.Fatal(err)
	}
	h := s.RegisterRoutes()
	start := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/start", nil)
	sw := httptest.NewRecorder()
	h.ServeHTTP(sw, start)
	if sw.Code != http.StatusFound {
		t.Fatalf("start %d", sw.Code)
	}
	loc, err := url.Parse(sw.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	q := loc.Query()
	if q.Get("client_id") != idp.clientID || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != idp.redirect || q.Get("code_challenge") == "" {
		t.Fatalf("authorize query = %s", loc.RawQuery)
	}
	if strings.Contains(loc.RawQuery, "secret") || strings.Contains(loc.RawQuery, "access_token") {
		t.Fatalf("authorize URL carries a secret: %s", loc.RawQuery)
	}
	var stateCookie *http.Cookie
	for _, c := range sw.Result().Cookies() {
		if c.Name == "goxio_oidc" {
			stateCookie = c
		}
	}
	if stateCookie == nil || !stateCookie.HttpOnly || stateCookie.SameSite != http.SameSiteLaxMode || stateCookie.Value != q.Get("state") {
		t.Fatalf("state cookie %#v", stateCookie)
	}
	idp.addCode("code-1", q.Get("code_challenge"), map[string]any{
		"iss": idp.srv.URL, "sub": "subject-1", "aud": idp.clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": q.Get("nonce"),
		"email": "owner@example.com",
	})
	cb := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/callback?code=code-1&state="+q.Get("state"), nil)
	cb.AddCookie(stateCookie)
	cw := httptest.NewRecorder()
	h.ServeHTTP(cw, cb)
	if cw.Code != http.StatusFound || cw.Header().Get("Location") != "/" {
		t.Fatalf("callback %d %s body %s", cw.Code, cw.Header().Get("Location"), cw.Body.String())
	}
	if strings.Contains(cw.Body.String(), "should-not-leak") || strings.Contains(cw.Header().Get("Set-Cookie"), "should-not-leak") {
		t.Fatal("access token reached the browser")
	}
	var session *http.Cookie
	for _, c := range cw.Result().Cookies() {
		if c.Name == "goxio_session" && c.Value != "" && c.MaxAge >= 0 {
			session = c
		}
	}
	if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session %#v", session)
	}
	acct, err := p.FindOIDCAccount(idp.srv.URL, "subject-1")
	if err != nil || acct.ID == owner.AccountID || acct.Login == "owner@example.com" {
		t.Fatalf("sso account = %+v err %v", acct.Login, err)
	}
	if _, err := p.Authenticate(acct.Login, "password-owner-ok"); err != portal.ErrInvalid {
		t.Fatal("sso account accepted the local password")
	}
	list, err := p.ListServers(acct.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("sso servers = %+v %v", list, err)
	}
	greq := httptest.NewRequest(http.MethodGet, "/api/graph?server="+box.ID, nil)
	greq.AddCookie(session)
	greq.Header.Set("X-GoXIO-Request", "1")
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, greq)
	if gw.Code != http.StatusNotFound || strings.Contains(gw.Body.String(), "owner-only-marker") {
		t.Fatalf("sso graph %d %s", gw.Code, gw.Body.String())
	}
	if _, err := p.Authenticate("OWNER@example.com", "password-owner-ok"); err != nil {
		t.Fatal("local login broke")
	}
	again := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/callback?code=code-1&state="+q.Get("state"), nil)
	again.AddCookie(stateCookie)
	aw := httptest.NewRecorder()
	h.ServeHTTP(aw, again)
	if aw.Code != http.StatusFound || aw.Header().Get("Location") != "/?sso_error=rejected" {
		t.Fatalf("replay %d %s", aw.Code, aw.Header().Get("Location"))
	}
	p2, err := portal.Open(dir, portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p2.LookupSession(session.Value); err != nil {
		t.Fatal(err)
	}
	if _, err := p2.FindOIDCAccount(idp.srv.URL, "subject-1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p2.Graph(acct.ID, box.ID); err == nil {
		t.Fatal("restarted store let the sso account read the owner server")
	}
}

func TestOIDCRejectsWrongIssuerAudienceNonceAndReuse(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	idp := newFakeIDP(t)
	idp.wire(s)
	h := s.RegisterRoutes()
	begin := func() (url.Values, *http.Cookie) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/start", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		loc, err := url.Parse(w.Header().Get("Location"))
		if err != nil || w.Code != http.StatusFound {
			t.Fatalf("start %d %v", w.Code, err)
		}
		var c *http.Cookie
		for _, ck := range w.Result().Cookies() {
			if ck.Name == "goxio_oidc" {
				c = ck
			}
		}
		return loc.Query(), c
	}
	finish := func(q url.Values, c *http.Cookie, code string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/callback?code="+code+"&state="+q.Get("state"), nil)
		if c != nil {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Header().Get("Location")
	}
	base := func(q url.Values, sub, iss, aud, nonce string) map[string]any {
		return map[string]any{
			"iss": iss, "sub": sub, "aud": aud, "nonce": nonce,
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"email": "owner@example.com",
		}
	}
	q, c := begin()
	req := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/callback?code=x&state=not-the-state", nil)
	req.AddCookie(c)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Header().Get("Location") != "/?sso_error=rejected" {
		t.Fatalf("bad state %s", w.Header().Get("Location"))
	}
	// The first pending was not consumed by the bad state. Use it for audience.
	idp.addCode("aud", q.Get("code_challenge"), base(q, "s-aud", idp.srv.URL, "other-client", q.Get("nonce")))
	if loc := finish(q, c, "aud"); loc != "/?sso_error=rejected" {
		t.Fatalf("bad aud %s", loc)
	}
	q, c = begin()
	idp.addCode("iss", q.Get("code_challenge"), base(q, "s-iss", "http://evil.example", idp.clientID, q.Get("nonce")))
	if loc := finish(q, c, "iss"); loc != "/?sso_error=rejected" {
		t.Fatalf("bad iss %s", loc)
	}
	q, c = begin()
	idp.addCode("nonce", q.Get("code_challenge"), base(q, "s-nonce", idp.srv.URL, idp.clientID, "other-nonce"))
	if loc := finish(q, c, "nonce"); loc != "/?sso_error=rejected" {
		t.Fatalf("bad nonce %s", loc)
	}
	q, c = begin()
	idp.addCode("once", q.Get("code_challenge"), base(q, "s-once", idp.srv.URL, idp.clientID, q.Get("nonce")))
	if loc := finish(q, c, "once"); loc != "/" {
		t.Fatalf("good code %s", loc)
	}
	q2, c2 := begin()
	if loc := finish(q2, c2, "once"); loc != "/?sso_error=unavailable" {
		t.Fatalf("reused code %s", loc)
	}
}

func TestOIDCLinkIsExplicitAndProviderOutageKeepsLocalLogin(t *testing.T) {
	dir := t.TempDir()
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(dir, portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	idp := newFakeIDP(t)
	idp.wire(s)
	if _, err := p.CreateAccount("local-user", "password-local-ok"); err != nil {
		t.Fatal(err)
	}
	local, err := p.Authenticate("local-user", "password-local-ok")
	if err != nil {
		t.Fatal(err)
	}
	h := s.RegisterRoutes()
	link := httptest.NewRequest(http.MethodPost, "/api/portal/oidc/link", strings.NewReader("csrf="+local.CSRF))
	link.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	link.AddCookie(&http.Cookie{Name: "goxio_session", Value: local.ID})
	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, link)
	if lw.Code != http.StatusFound {
		t.Fatalf("link %d %s", lw.Code, lw.Body.String())
	}
	loc, _ := url.Parse(lw.Header().Get("Location"))
	q := loc.Query()
	var stateCookie *http.Cookie
	for _, c := range lw.Result().Cookies() {
		if c.Name == "goxio_oidc" {
			stateCookie = c
		}
	}
	idp.addCode("link-code", q.Get("code_challenge"), map[string]any{
		"iss": idp.srv.URL, "sub": "linked-subject", "aud": idp.clientID, "nonce": q.Get("nonce"),
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "email": "local-user",
	})
	cb := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/callback?code=link-code&state="+q.Get("state"), nil)
	cb.AddCookie(stateCookie)
	cw := httptest.NewRecorder()
	h.ServeHTTP(cw, cb)
	if cw.Header().Get("Location") != "/" {
		t.Fatalf("link callback %s", cw.Header().Get("Location"))
	}
	bound, err := p.FindOIDCAccount(idp.srv.URL, "linked-subject")
	if err != nil || bound.ID != local.AccountID {
		t.Fatalf("bound = %s err %v", bound.ID, err)
	}
	if _, err := p.CreateAccount("other-user", "password-other-ok"); err != nil {
		t.Fatal(err)
	}
	other, err := p.Authenticate("other-user", "password-other-ok")
	if err != nil {
		t.Fatal(err)
	}
	link = httptest.NewRequest(http.MethodPost, "/api/portal/oidc/link", strings.NewReader("csrf="+other.CSRF))
	link.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	link.AddCookie(&http.Cookie{Name: "goxio_session", Value: other.ID})
	lw = httptest.NewRecorder()
	h.ServeHTTP(lw, link)
	loc, _ = url.Parse(lw.Header().Get("Location"))
	q = loc.Query()
	stateCookie = nil
	for _, c := range lw.Result().Cookies() {
		if c.Name == "goxio_oidc" {
			stateCookie = c
		}
	}
	idp.addCode("steal", q.Get("code_challenge"), map[string]any{
		"iss": idp.srv.URL, "sub": "linked-subject", "aud": idp.clientID, "nonce": q.Get("nonce"),
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	})
	cb = httptest.NewRequest(http.MethodGet, "/api/portal/oidc/callback?code=steal&state="+q.Get("state"), nil)
	cb.AddCookie(stateCookie)
	cw = httptest.NewRecorder()
	h.ServeHTTP(cw, cb)
	if cw.Header().Get("Location") != "/?sso_error=rejected" {
		t.Fatalf("takeover %s", cw.Header().Get("Location"))
	}
	still, err := p.FindOIDCAccount(idp.srv.URL, "linked-subject")
	if err != nil || still.ID != local.AccountID {
		t.Fatal("subject moved")
	}

	down := newOIDC(oidcConfig{Issuer: "http://127.0.0.1:1", ClientID: "goxio-client", ClientSecret: "x", RedirectURI: "http://127.0.0.1/api/portal/oidc/callback"})
	s.oidc = down
	req := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/start", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Header().Get("Location") != "/?sso_error=unavailable" {
		t.Fatalf("outage %s", w.Header().Get("Location"))
	}
	login := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"local-user","password":"password-local-ok"}`))
	login.Header.Set("X-GoXIO-Request", "1")
	login.RemoteAddr = "203.0.113.40:40"
	lw = httptest.NewRecorder()
	h.ServeHTTP(lw, login)
	if lw.Code != http.StatusOK {
		t.Fatalf("local login during outage %d %s", lw.Code, lw.Body.String())
	}
}

func TestOIDCDiscoveryRejectsForeignTokenHost(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("foreign token host was contacted")
	}))
	defer foreign.Close()
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 "",
			"authorization_endpoint": foreign.URL + "/authorize",
			"token_endpoint":         foreign.URL + "/token",
			"jwks_uri":               foreign.URL + "/jwks",
		})
	}))
	defer issuer.Close()
	// Fix issuer field to the real issuer URL after the server exists.
	issuer.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 issuer.URL,
			"authorization_endpoint": issuer.URL + "/authorize",
			"token_endpoint":         foreign.URL + "/token",
			"jwks_uri":               issuer.URL + "/jwks",
		})
	})
	s := newTestServer(&mockRegistry{graph: testGraph()})
	p, err := portal.Open(t.TempDir(), portal.Options{BcryptCost: bcrypt.MinCost, SecureCookies: false})
	if err != nil {
		t.Fatal(err)
	}
	s.portal = p
	s.oidc = newOIDC(oidcConfig{Issuer: issuer.URL, ClientID: "goxio-client", ClientSecret: "secret", RedirectURI: "http://127.0.0.1/cb"})
	req := httptest.NewRequest(http.MethodGet, "/api/portal/oidc/start", nil)
	w := httptest.NewRecorder()
	s.RegisterRoutes().ServeHTTP(w, req)
	if w.Header().Get("Location") != "/?sso_error=unavailable" {
		t.Fatalf("foreign discovery %s", w.Header().Get("Location"))
	}
}
