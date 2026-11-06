package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpaceRoutesRequireSignIn(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	h := s.RegisterRoutes()
	req := httptest.NewRequest(http.MethodPost, "/api/space/actions", strings.NewReader(`{"node_id":"space-volume-old-cache","action":"delete","confirm":"DELETE"}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "old-cache") || strings.Contains(w.Body.String(), "pending_delete") {
		t.Fatalf("unauthenticated action leaked: %s", w.Body.String())
	}
}
