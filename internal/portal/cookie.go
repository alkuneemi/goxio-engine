package portal

import (
	"net/http"
)

func (s *Store) CookieName() string {
	if s.opt.SecureCookies {
		return "__Host-goxio_session"
	}
	return "goxio_session"
}

func (s *Store) WriteSessionCookie(w http.ResponseWriter, sess Session) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName(),
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.opt.SecureCookies,
		SameSite: http.SameSiteStrictMode,
		Expires:  sess.ExpiresAt,
		MaxAge:   int(s.opt.SessionTTL.Seconds()),
	})
}

func (s *Store) ClearSessionCookie(w http.ResponseWriter) {
	for _, name := range []string{"__Host-goxio_session", "goxio_session"} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   name == "__Host-goxio_session",
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
	}
}

func (s *Store) ReadSessionCookie(r *http.Request) string {
	c, err := r.Cookie(s.CookieName())
	if err != nil {
		return ""
	}
	return c.Value
}
