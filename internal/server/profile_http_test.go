package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func TestPortalProfileSetAndMe(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: &graph.Graph{}})
	cookie, _ := seedPortal(t, s, "sahabify", "password-owner-ok", space.Snapshot{})
	csrf := csrfOf(t, s, cookie)
	h := s.RegisterRoutes()

	req := httptest.NewRequest(http.MethodPut, "/api/portal/profile", strings.NewReader(`{"company_name":"سحاب الحلول الذكية"}`))
	authed(req, cookie, csrf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put status %d %s", w.Code, w.Body.String())
	}
	var putBody struct {
		Data struct {
			CompanyName  string `json:"company_name"`
			Login        string `json:"login"`
			ID           string `json:"id"`
			Password     string `json:"password"`
			PasswordHash string `json:"password_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &putBody); err != nil {
		t.Fatal(err)
	}
	if putBody.Data.CompanyName != "سحاب الحلول الذكية" || putBody.Data.Login != "sahabify" {
		t.Fatalf("put data %+v", putBody.Data)
	}
	if putBody.Data.Password != "" || putBody.Data.PasswordHash != "" {
		t.Fatal("password leaked")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/portal/me", nil)
	authed(req, cookie, "")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("me %d %s", w.Code, w.Body.String())
	}
	var meBody struct {
		Data struct {
			CompanyName  string `json:"company_name"`
			Login        string `json:"login"`
			CSRF         string `json:"csrf"`
			ServerCount  int    `json:"server_count"`
			PasswordHash string `json:"password_hash"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &meBody); err != nil {
		t.Fatal(err)
	}
	if meBody.Data.CompanyName != "سحاب الحلول الذكية" || meBody.Data.CSRF == "" || meBody.Data.ServerCount != 1 {
		t.Fatalf("me %+v", meBody.Data)
	}
	if meBody.Data.PasswordHash != "" {
		t.Fatal("hash leaked from me")
	}

	req = httptest.NewRequest(http.MethodPut, "/api/portal/profile", strings.NewReader(`{"company_name":"`+strings.Repeat("x", 81)+`"}`))
	authed(req, cookie, csrf)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("long name status %d", w.Code)
	}
}
