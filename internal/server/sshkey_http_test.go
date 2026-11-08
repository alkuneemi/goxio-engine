package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func testAuthorizedKey(t *testing.T, comment string) (string, string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		line += " " + comment
	}
	return line, ssh.FingerprintSHA256(pub)
}

func jsonString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestPortalServerSSHKeySetReplaceDelete(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: &graph.Graph{}})
	cookie, serverID := seedPortal(t, s, "owner", "password-owner-ok", space.Snapshot{})
	csrf := csrfOf(t, s, cookie)
	h := s.RegisterRoutes()

	first, firstFP := testAuthorizedKey(t, "first")
	req := httptest.NewRequest(http.MethodPut, "/api/portal/servers/"+serverID+"/ssh-key", strings.NewReader(`{"ssh_public_key":`+jsonString(first)+`}`))
	authed(req, cookie, csrf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set status %d %s", w.Code, w.Body.String())
	}
	var setBody struct {
		Data struct {
			SSHPublicKey   string `json:"ssh_public_key"`
			SSHFingerprint string `json:"ssh_fingerprint"`
			IngestHash     string `json:"ingest_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &setBody); err != nil {
		t.Fatal(err)
	}
	if setBody.Data.SSHFingerprint != firstFP {
		t.Fatalf("fp %s want %s", setBody.Data.SSHFingerprint, firstFP)
	}
	if setBody.Data.IngestHash != "" {
		t.Fatal("ingest hash leaked")
	}
	if setBody.Data.SSHPublicKey == "" {
		t.Fatal("missing public key in view")
	}

	second, secondFP := testAuthorizedKey(t, "second")
	req = httptest.NewRequest(http.MethodPut, "/api/portal/servers/"+serverID+"/ssh-key", strings.NewReader(`{"ssh_public_key":`+jsonString(second)+`}`))
	authed(req, cookie, csrf)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("replace status %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &setBody); err != nil {
		t.Fatal(err)
	}
	if setBody.Data.SSHFingerprint != secondFP {
		t.Fatalf("replaced fp %s want %s", setBody.Data.SSHFingerprint, secondFP)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/portal/servers", nil)
	authed(req, cookie, "")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d %s", w.Code, w.Body.String())
	}
	var listBody struct {
		Data []struct {
			SSHFingerprint string `json:"ssh_fingerprint"`
			IngestHash     string `json:"ingest_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Data) != 1 || listBody.Data[0].SSHFingerprint != secondFP || listBody.Data[0].IngestHash != "" {
		t.Fatalf("list = %+v", listBody.Data)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/portal/servers/"+serverID+"/ssh-key", strings.NewReader(`{}`))
	authed(req, cookie, csrf)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status %d %s", w.Code, w.Body.String())
	}
	var deleted struct {
		Data struct {
			SSHPublicKey   string `json:"ssh_public_key"`
			SSHFingerprint string `json:"ssh_fingerprint"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &deleted); err != nil {
		t.Fatal(err)
	}
	if deleted.Data.SSHPublicKey != "" || deleted.Data.SSHFingerprint != "" {
		t.Fatalf("delete left %+v body=%s", deleted.Data, w.Body.String())
	}
}

func TestPortalServerSSHKeyRejectsPrivateAndCrossTenant(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: &graph.Graph{}})
	cookie, serverID := seedPortal(t, s, "owner", "password-owner-ok", space.Snapshot{})
	csrf := csrfOf(t, s, cookie)
	h := s.RegisterRoutes()

	req := httptest.NewRequest(http.MethodPut, "/api/portal/servers/"+serverID+"/ssh-key", strings.NewReader(`{"ssh_public_key":"-----BEGIN OPENSSH PRIVATE KEY-----\nxxxx\n-----END OPENSSH PRIVATE KEY-----"}`))
	authed(req, cookie, csrf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "private ssh keys are not accepted") {
		t.Fatalf("private key status %d %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/portal/servers/"+serverID+"/ssh-key", strings.NewReader(`{"ssh_public_key":"ssh-ed25519 AAAA owner"}`))
	authed(req, cookie, "")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status %d %s", w.Code, w.Body.String())
	}

	if _, err := s.portal.CreateAccount("beta", "password-beta-okxx"); err != nil {
		t.Fatal(err)
	}
	login := httptest.NewRequest(http.MethodPost, "/api/portal/login", strings.NewReader(`{"login":"beta","password":"password-beta-okxx"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("X-GoXIO-Request", "1")
	login.RemoteAddr = "198.51.100.9:40000"
	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, login)
	if lw.Code != http.StatusOK {
		t.Fatalf("beta login %d %s", lw.Code, lw.Body.String())
	}
	var otherCookie *http.Cookie
	for _, c := range lw.Result().Cookies() {
		if c.Name == "goxio_session" {
			otherCookie = c
		}
	}
	if otherCookie == nil {
		t.Fatal("beta session cookie missing")
	}
	otherCSRF := csrfOf(t, s, otherCookie)
	pub, _ := testAuthorizedKey(t, "x")
	req = httptest.NewRequest(http.MethodPut, "/api/portal/servers/"+serverID+"/ssh-key", strings.NewReader(`{"ssh_public_key":`+jsonString(pub)+`}`))
	authed(req, otherCookie, otherCSRF)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant status %d %s", w.Code, w.Body.String())
	}
}
