package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func (s *Server) currentGraph() (*graph.Graph, error) {
	g, err := s.registry.DiscoverAll()
	if err != nil {
		return nil, err
	}
	if s.space == nil {
		return g, nil
	}
	return s.space.Merge(g), nil
}

func (s *Server) spaceSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if s.space == nil {
		http.Error(w, "space map unavailable", http.StatusNotFound)
		return
	}
	writeData(w, s.logger, s.space.Summary())
}

func (s *Server) spaceActionsHandler(w http.ResponseWriter, r *http.Request) {
	if s.space == nil {
		http.Error(w, "space map unavailable", http.StatusNotFound)
		return
	}
	writeData(w, s.logger, s.space.Actions())
}

func (s *Server) spaceActionHandler(w http.ResponseWriter, r *http.Request) {
	if s.space == nil {
		http.Error(w, "space map unavailable", http.StatusNotFound)
		return
	}
	var req space.ActionRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid action json")
		return
	}
	rec, err := s.space.Apply(req)
	if err != nil {
		writeSpaceError(w, err)
		return
	}
	s.logger.Infow("space action recorded", "action", rec.Action, "node", rec.NodeID, "applied_to_host", rec.AppliedToHost)
	writeData(w, s.logger, rec)
}

func (s *Server) spaceIngestHandler(w http.ResponseWriter, r *http.Request) {
	if s.space == nil {
		http.Error(w, "space map unavailable", http.StatusNotFound)
		return
	}
	token := r.Header.Get("X-Goxio-Token")
	if token == "" {
		auth := r.Header.Get("Authorization")
		token = strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
			token = ""
		}
	}
	if err := s.space.Ingest(r.Body, token); err != nil {
		writeSpaceError(w, err)
		return
	}
	writeData(w, s.logger, s.space.Summary())
}

func writeSpaceError(w http.ResponseWriter, err error) {
	var se *space.StatusError
	if errors.As(err, &se) {
		writeAPIError(w, se.Code, se.Msg)
		return
	}
	writeAPIError(w, http.StatusBadRequest, "request rejected")
}

func writeAPIError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeData(w http.ResponseWriter, logger *zap.SugaredLogger, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": v}); err != nil && logger != nil {
		logger.Errorw("encode failed", "err", err)
	}
}
