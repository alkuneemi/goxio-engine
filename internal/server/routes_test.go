package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/guilherme-grimm/graph-go/internal/adapters"
	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/health"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// mockRegistry implements adapters.Registry for testing.
type mockRegistry struct {
	graph   *graph.Graph
	health  []health.HealthMetrics
	discErr error
}

func (m *mockRegistry) Register(string, string, adapters.Adapter, adapters.ConnectionConfig) error {
	return nil
}
func (m *mockRegistry) Unregister(string) error                        { return nil }
func (m *mockRegistry) Get(string) (adapters.Adapter, bool)            { return nil, false }
func (m *mockRegistry) Names() []string                                { return nil }
func (m *mockRegistry) DiscoverAll() (*graph.Graph, error)             { return m.graph, m.discErr }
func (m *mockRegistry) HealthAll() []health.HealthMetrics              { return m.health }
func (m *mockRegistry) InvalidateCache()                               {}
func (m *mockRegistry) SetTopology(string, []nodes.Node, []edges.Edge) {}
func (m *mockRegistry) CloseAll() error                                { return nil }

func testGraph() *graph.Graph {
	return &graph.Graph{
		Nodes: []nodes.Node{
			{Id: "pg-test", Type: "database", Name: "test", Health: "healthy", Metadata: map[string]any{}},
			{Id: "pg-test-users", Type: "table", Name: "users", Parent: "pg-test", Health: "healthy", Metadata: map[string]any{}},
		},
		Edges: []edges.Edge{
			{Id: "fk-1", Source: "pg-test-users", Target: "pg-test", Type: "foreign_key"},
		},
	}
}

func newTestServer(reg adapters.Registry) *Server {
	return &Server{registry: reg, logger: testLogger}
}

func TestGraphHandler(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	srv := httptest.NewServer(http.HandlerFunc(s.graphHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGraphHandler_EmptyAdapters(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: &graph.Graph{
		Nodes: make([]nodes.Node, 0),
		Edges: make([]edges.Edge, 0),
	}})
	srv := httptest.NewServer(http.HandlerFunc(s.graphHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestNodeHandler_Found(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	r := s.RegisterRoutes()

	req := httptest.NewRequest("GET", "/api/node/pg-test-users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "pg-test") {
		t.Fatalf("unauthenticated node leaked: %s", w.Body.String())
	}
}

func TestNodeHandler_NotFound(t *testing.T) {
	s := newTestServer(&mockRegistry{graph: testGraph()})
	r := s.RegisterRoutes()

	req := httptest.NewRequest("GET", "/api/node/nonexistent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestApiHealthHandler_RequiresSignIn(t *testing.T) {
	s := newTestServer(&mockRegistry{
		health: []health.HealthMetrics{
			{NodeID: "postgres", Status: health.Healthy},
		},
	})
	srv := httptest.NewServer(http.HandlerFunc(s.apiHealthHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGraphHandler_Error(t *testing.T) {
	s := newTestServer(&mockRegistry{discErr: fmt.Errorf("connection refused")})
	srv := httptest.NewServer(http.HandlerFunc(s.graphHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHealthHandler(t *testing.T) {
	s := newTestServer(&mockRegistry{})
	srv := httptest.NewServer(http.HandlerFunc(s.healthHandler))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", body["status"])
	}
}
