package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/space"
)

func TestSpaceActionRouteRequiresConfirm(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	st := space.NewStore(zap.NewNop().Sugar())
	st.Load(space.Demo(time.Now()))
	s.space = st
	h := s.RegisterRoutes()

	req := httptest.NewRequest(http.MethodPost, "/api/space/actions", bytes.NewBufferString(`{"node_id":"space-volume-old-cache","action":"delete","confirm":""}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/space/actions", bytes.NewBufferString(`{"node_id":"space-volume-old-cache","action":"delete","confirm":"DELETE"}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			AppliedToHost bool   `json:"applied_to_host"`
			Action        string `json:"action"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Action != "delete" || body.Data.AppliedToHost {
		t.Fatalf("record = %+v", body.Data)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/graph", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("graph status = %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("space-volume-old-cache")) {
		t.Fatal("graph response missing space node")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("pending_delete")) {
		t.Fatal("graph response missing pending delete")
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("pg-test")) {
		t.Fatal("graph response dropped discovery node")
	}
}
