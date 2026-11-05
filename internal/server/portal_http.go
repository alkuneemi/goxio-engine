package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"github.com/guilherme-grimm/graph-go/internal/portal"
)

func (s *Server) requireSession(w http.ResponseWriter, r *http.Request) (portal.Session, bool) {
	if s.portal == nil {
		writeAPIError(w, http.StatusUnauthorized, "sign-in required")
		return portal.Session{}, false
	}
	sess, err := s.portal.LookupSession(s.portal.ReadSessionCookie(r))
	if err != nil {
		writeAPIError(w, http.StatusUnauthorized, "sign-in required")
		return portal.Session{}, false
	}
	return sess, true
}

func (s *Server) requireServer(w http.ResponseWriter, r *http.Request, sess portal.Session) (portal.Server, bool) {
	id := r.URL.Query().Get("server")
	if id == "" {
		id = mux.Vars(r)["id"]
	}
	if !portal.SafeID(id) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return portal.Server{}, false
	}
	srv, err := s.portal.LookupServer(sess.AccountID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return portal.Server{}, false
	}
	return srv, true
}

func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request, sess portal.Session) bool {
	if err := s.portal.CheckCSRF(sess, r.Header.Get("X-CSRF-Token")); err != nil {
		writeAPIError(w, http.StatusForbidden, "csrf check failed")
		return false
	}
	return true
}

func wantBrowser(r *http.Request) bool {
	return r.Header.Get("X-GoXIO-Request") == "1"
}

func (s *Server) portalLoginHandler(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil {
		writeAPIError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	if !wantBrowser(r) {
		writeAPIError(w, http.StatusBadRequest, "sign-in rejected")
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	if err := dec.Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid login")
		return
	}
	ip := portal.ClientIP(r, s.portal.TrustProxy())
	key := ip + "|" + strings.ToLower(strings.TrimSpace(body.Login))
	if err := s.portal.AllowLogin(key); err != nil {
		writeAPIError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}
	sess, err := s.portal.Authenticate(body.Login, body.Password)
	if err != nil {
		s.portal.NoteLoginFail(key)
		writeAPIError(w, http.StatusUnauthorized, "invalid login or password")
		return
	}
	s.portal.ClearLoginFail(key)
	s.portal.WriteSessionCookie(w, sess)
	writeData(w, s.logger, map[string]any{
		"login":      strings.ToLower(strings.TrimSpace(body.Login)),
		"csrf":       sess.CSRF,
		"expires_at": sess.ExpiresAt,
	})
}

func (s *Server) portalLogoutHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if !wantBrowser(r) || !s.requireCSRF(w, r, sess) {
		if wantBrowser(r) {
			return
		}
		writeAPIError(w, http.StatusBadRequest, "sign-out rejected")
		return
	}
	s.portal.Revoke(sess.ID)
	s.portal.ClearSessionCookie(w)
	writeData(w, s.logger, map[string]any{"ok": true})
}

func (s *Server) portalMeHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	writeData(w, s.logger, map[string]any{
		"csrf":       sess.CSRF,
		"expires_at": sess.ExpiresAt,
	})
}

func (s *Server) portalServersHandler(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	list, err := s.portal.ListServers(sess.AccountID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	writeData(w, s.logger, list)
}

func (s *Server) portalRefreshHandler(w http.ResponseWriter, r *http.Request) {
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
	rec, err := s.portal.RequestRefresh(sess.AccountID, srv.ID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	// Receipt only. The engine does not dial the address or use an SSH key.
	writeData(w, s.logger, map[string]any{
		"id":        rec.ID,
		"server_id": rec.ServerID,
		"status":    rec.Status,
		"at":        rec.At,
	})
}
