package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/guilherme-grimm/graph-go/internal/portal"
)

const loginHelp = "لم تُقبل بيانات الدخول. لم تُفتح جلسة. كلمة المرور تُقارن كما أُرسلت، بما فيها المسافة وحالة الأحرف. لا يُرسل بريد لإعادة التعيين. That login or password was not accepted. No session was opened. The password is compared exactly, including spaces and letter case. GoXIO does not email a reset."

const registerTakenHelp = "هذا الدخول مستخدم. لم يُربط بحساب موجود ولم تُفتح جلسة. اختر دخولًا آخر. That login is already in use. It was not attached to an existing account and no session was opened. Choose a different login."

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
	if !sameSiteBrowser(r, s.portal.TrustedProxies()) {
		writeAPIError(w, http.StatusBadRequest, "sign-in rejected")
		return
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid login")
		return
	}
	ip := portal.ClientIP(r, s.portal.TrustedProxies())
	key := "login|" + ip + "|" + portal.LoginKey(body.Login)
	if err := s.portal.AllowLogin(key); err != nil {
		writeRateLimited(w, s.portal.RetryAfter(key))
		return
	}
	sess, err := s.portal.Authenticate(body.Login, body.Password)
	if err != nil {
		if !errors.Is(err, portal.ErrInvalid) {
			writeAPIError(w, http.StatusInternalServerError, "sign-in unavailable")
			return
		}
		s.portal.NoteLoginFail(key)
		writeLoginDenied(w)
		return
	}
	s.portal.ClearLoginFail(key)
	s.portal.WriteSessionCookie(w, sess)
	writeData(w, s.logger, map[string]any{
		"login":      portal.LoginKey(body.Login),
		"csrf":       sess.CSRF,
		"expires_at": sess.ExpiresAt,
	})
}

func (s *Server) portalRegisterHandler(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil {
		writeAPIError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	if !sameSiteBrowser(r, s.portal.TrustedProxies()) {
		writeAPIError(w, http.StatusBadRequest, "registration rejected")
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid registration")
		return
	}
	key := "signup|" + portal.ClientIP(r, s.portal.TrustedProxies()) + "|" + portal.LoginKey(body.Login)
	if err := s.portal.AllowLogin(key); err != nil {
		writeRateLimited(w, s.portal.RetryAfter(key))
		return
	}
	sess, err := s.portal.Register(body.Login, body.Password, body.Confirm)
	if err != nil {
		s.portal.NoteLoginFail(key)
		if errors.Is(err, portal.ErrExists) {
			writeStatusJSON(w, http.StatusConflict, map[string]string{"error": "that login is already in use", "help": registerTakenHelp})
			return
		}
		if portal.SignInPending(err) {
			// 503, not 200. The account exists and no session cookie is set.
			writeAPIError(w, http.StatusServiceUnavailable, "account created; sign-in did not complete")
			return
		}
		var pe *portal.PolicyError
		if errors.As(err, &pe) {
			writeAPIError(w, http.StatusBadRequest, pe.Error())
			return
		}
		msg := err.Error()
		if strings.Contains(msg, "login") || strings.Contains(msg, "email") {
			writeAPIError(w, http.StatusBadRequest, msg)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "registration unavailable")
		return
	}
	s.portal.ClearLoginFail(key)
	s.portal.WriteSessionCookie(w, sess)
	list, err := s.portal.ListServers(sess.AccountID)
	if err != nil {
		list = []portal.ServerView{}
	}
	writeData(w, s.logger, map[string]any{
		"login":      portal.LoginKey(body.Login),
		"csrf":       sess.CSRF,
		"expires_at": sess.ExpiresAt,
		"status":     "empty",
		"servers":    list,
		"detail":     "الحساب جديد وفارغ. لا خوادم ولا جرد. لم يُنسخ أي مفتاح. The account is new and empty. It has no servers and no inventory. No key was copied.",
	})
}

func writeLoginDenied(w http.ResponseWriter) {
	writeStatusJSON(w, http.StatusUnauthorized, map[string]string{
		"error": "invalid login or password",
		"help":  loginHelp,
	})
}

func writeStatusJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRateLimited(w http.ResponseWriter, seconds int) {
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeAPIError(w, http.StatusTooManyRequests, "too many attempts")
}

func sameSiteBrowser(r *http.Request, trust *portal.TrustedProxies) bool {
	if r.Header.Get("X-GoXIO-Request") != "1" {
		return false
	}
	host := portal.ForwardedHost(r, trust)
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		ref := strings.TrimSpace(r.Header.Get("Referer"))
		if ref == "" {
			return true
		}
		u, err := url.Parse(ref)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, host)
	}
	if strings.EqualFold(origin, "null") {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return strings.EqualFold(u.Host, host)
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

func (s *Server) portalServerSSHKeyHandler(w http.ResponseWriter, r *http.Request) {
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
	switch r.Method {
	case http.MethodPut:
		var body struct {
			SSHPublicKey string `json:"ssh_public_key"`
		}
		dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid ssh public key")
			return
		}
		view, err := s.portal.SetServerSSHPublicKey(sess.AccountID, srv.ID, body.SSHPublicKey)
		if err != nil {
			writeSSHKeyError(w, err)
			return
		}
		writeData(w, s.logger, view)
	case http.MethodDelete:
		view, err := s.portal.DeleteServerSSHPublicKey(sess.AccountID, srv.ID)
		if err != nil {
			writeSSHKeyError(w, err)
			return
		}
		writeData(w, s.logger, view)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func writeSSHKeyError(w http.ResponseWriter, err error) {
	if errors.Is(err, portal.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, portal.ErrSSHPrivateKey) || errors.Is(err, portal.ErrSSHInvalid) || errors.Is(err, portal.ErrSSHUnsupported) {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "ssh public key unavailable")
}
