package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/portal"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func (s *Server) spaceSummaryHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	srv, ok := s.requireServer(w, r, sess)
	if !ok {
		return
	}
	_, meta, err := s.portal.Graph(sess.AccountID, srv.ID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	writeData(w, s.logger, meta)
}

func (s *Server) spaceActionsHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if _, ok := s.requireServer(w, r, sess); !ok {
		return
	}
	writeData(w, s.logger, []any{})
}

func (s *Server) spaceActionHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, sess) {
		return
	}
	srv, ok := s.requireServer(w, r, sess)
	if !ok {
		return
	}
	var req space.ActionRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid action json")
		return
	}
	rec, err := s.portal.Apply(sess.AccountID, srv.ID, req)
	if err != nil {
		var se *space.StatusError
		if errors.As(err, &se) {
			writeAPIError(w, se.Code, se.Msg)
			return
		}
		if errors.Is(err, portal.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "not found")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "request rejected")
		return
	}
	s.logger.Infow("space action recorded", "action", rec.Action, "node", rec.NodeID, "applied_to_host", rec.AppliedToHost)
	writeData(w, s.logger, rec)
}

func (s *Server) spaceIngestHandler(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil {
		writeAPIError(w, http.StatusUnauthorized, "sign-in required")
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
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid snapshot json")
		return
	}
	meta, err := s.portal.Ingest(token, body)
	if err != nil {
		if errors.Is(err, portal.ErrToken) {
			writeAPIError(w, http.StatusUnauthorized, "invalid ingest token")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid snapshot json")
		return
	}
	writeData(w, s.logger, meta)
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
