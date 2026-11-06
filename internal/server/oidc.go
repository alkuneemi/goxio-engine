package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/portal"
)

// oidcConfig is the operator's relying-party registration.
// The issuer is this value only. A request cannot choose a different one.
type oidcConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

type oidcRuntime struct {
	cfg    oidcConfig
	client *http.Client

	mu     sync.Mutex
	doc    *oidcDoc
	docAt  time.Time
	keys   map[string]*rsa.PublicKey
	keysAt time.Time
}

type oidcDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

func oidcConfigFromEnv() (oidcConfig, bool) {
	secret := os.Getenv("GOXIO_OIDC_CLIENT_SECRET")
	if secret == "" {
		if path := os.Getenv("GOXIO_OIDC_CLIENT_SECRET_FILE"); path != "" {
			b, err := os.ReadFile(path)
			if err != nil {
				return oidcConfig{}, false
			}
			secret = strings.TrimSuffix(string(b), "\n")
		}
	}
	cfg := oidcConfig{
		Issuer:       strings.TrimRight(strings.TrimSpace(os.Getenv("GOXIO_OIDC_ISSUER")), "/"),
		ClientID:     strings.TrimSpace(os.Getenv("GOXIO_OIDC_CLIENT_ID")),
		ClientSecret: secret,
		RedirectURI:  strings.TrimSpace(os.Getenv("GOXIO_OIDC_REDIRECT_URI")),
	}
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" {
		return oidcConfig{}, false
	}
	if _, err := endpointURL(cfg.Issuer, cfg.Issuer); err != nil {
		return oidcConfig{}, false
	}
	return cfg, true
}

func newOIDC(cfg oidcConfig) *oidcRuntime {
	o := &oidcRuntime{cfg: cfg}
	o.client = &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 2 {
				return errors.New("oidc redirect refused")
			}
			if _, err := endpointURL(cfg.Issuer, req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
	return o
}

func (o *oidcRuntime) enabled() bool { return o != nil && o.cfg.Issuer != "" }

func (s *Server) portalOIDCInfo(w http.ResponseWriter, r *http.Request) {
	writeData(w, s.logger, map[string]any{"configured": s.oidc.enabled()})
}

func (s *Server) portalOIDCStart(w http.ResponseWriter, r *http.Request) {
	s.beginOIDC(w, r, "")
}

func (s *Server) portalOIDCLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sess, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if err := s.portal.CheckCSRF(sess, firstNonEmpty(r.Header.Get("X-CSRF-Token"), r.FormValue("csrf"))); err != nil {
		writeAPIError(w, http.StatusForbidden, "csrf check failed")
		return
	}
	s.beginOIDC(w, r, sess.AccountID)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) beginOIDC(w http.ResponseWriter, r *http.Request, linkAccount string) {
	if s.portal == nil || !s.oidc.enabled() {
		ssoRedirect(w, r, "unconfigured")
		return
	}
	doc, err := s.oidc.discover(r.Context())
	if err != nil {
		ssoRedirect(w, r, "unavailable")
		return
	}
	state := randomHex(16)
	nonce := randomHex(16)
	verifier := randomHex(32)
	pending := portal.OIDCPending{
		State: state, Verifier: verifier, Nonce: nonce, AccountID: linkAccount,
		Expires: time.Now().Add(5 * time.Minute),
	}
	if err := s.portal.PutOIDCPending(pending); err != nil {
		ssoRedirect(w, r, "unavailable")
		return
	}
	s.writeOIDCStateCookie(w, state)
	auth, err := url.Parse(doc.AuthorizationEndpoint)
	if err != nil {
		ssoRedirect(w, r, "unavailable")
		return
	}
	q := auth.Query()
	q.Set("response_type", "code")
	q.Set("client_id", s.oidc.cfg.ClientID)
	q.Set("redirect_uri", s.oidc.cfg.RedirectURI)
	q.Set("scope", "openid")
	q.Set("state", state)
	q.Set("nonce", nonce)
	sum := sha256.Sum256([]byte(verifier))
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	auth.RawQuery = q.Encode()
	http.Redirect(w, r, auth.String(), http.StatusFound)
}

func (s *Server) portalOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.portal == nil || !s.oidc.enabled() {
		ssoRedirect(w, r, "unconfigured")
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" || state != s.readOIDCStateCookie(r) {
		ssoRedirect(w, r, "rejected")
		return
	}
	s.clearOIDCStateCookie(w)
	pending, err := s.portal.ConsumeOIDCPending(state)
	if err != nil {
		ssoRedirect(w, r, "rejected")
		return
	}
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		ssoRedirect(w, r, "rejected")
		return
	}
	rawToken, err := s.oidc.exchange(r.Context(), r.URL.Query().Get("code"), pending.Verifier)
	if err != nil {
		ssoRedirect(w, r, "unavailable")
		return
	}
	claims, err := s.oidc.verifyIDToken(r.Context(), rawToken, pending.Nonce)
	if err != nil {
		ssoRedirect(w, r, "rejected")
		return
	}
	acct, err := s.portal.FindOIDCAccount(s.oidc.cfg.Issuer, claims.Sub)
	if err != nil {
		if pending.AccountID != "" {
			if err := s.portal.BindOIDC(pending.AccountID, s.oidc.cfg.Issuer, claims.Sub); err != nil {
				ssoRedirect(w, r, "rejected")
				return
			}
			acct, err = s.portal.FindOIDCAccount(s.oidc.cfg.Issuer, claims.Sub)
		} else {
			acct, err = s.portal.CreateSSOAccount(s.oidc.cfg.Issuer, claims.Sub)
			if errors.Is(err, portal.ErrExists) {
				acct, err = s.portal.FindOIDCAccount(s.oidc.cfg.Issuer, claims.Sub)
			}
		}
	} else if pending.AccountID != "" && pending.AccountID != acct.ID {
		ssoRedirect(w, r, "rejected")
		return
	}
	if err != nil || acct.ID == "" {
		ssoRedirect(w, r, "unavailable")
		return
	}
	sess, err := s.portal.IssueSession(acct.ID)
	if err != nil {
		ssoRedirect(w, r, "unavailable")
		return
	}
	s.portal.WriteSessionCookie(w, sess)
	http.Redirect(w, r, "/", http.StatusFound)
}

func ssoRedirect(w http.ResponseWriter, r *http.Request, code string) {
	switch code {
	case "unconfigured", "unavailable", "rejected":
	default:
		code = "rejected"
	}
	http.Redirect(w, r, "/?sso_error="+code, http.StatusFound)
}

func (o *oidcRuntime) discover(ctx context.Context) (*oidcDoc, error) {
	o.mu.Lock()
	if o.doc != nil && time.Since(o.docAt) < 5*time.Minute {
		doc := o.doc
		o.mu.Unlock()
		return doc, nil
	}
	o.mu.Unlock()
	raw, err := o.get(ctx, strings.TrimRight(o.cfg.Issuer, "/")+"/.well-known/openid-configuration")
	if err != nil {
		return nil, err
	}
	var doc oidcDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc.Issuer != o.cfg.Issuer {
		return nil, errors.New("oidc issuer mismatch")
	}
	for _, ep := range []string{doc.AuthorizationEndpoint, doc.TokenEndpoint, doc.JWKSURI} {
		if _, err := endpointURL(o.cfg.Issuer, ep); err != nil {
			return nil, err
		}
	}
	o.mu.Lock()
	o.doc = &doc
	o.docAt = time.Now()
	o.mu.Unlock()
	return &doc, nil
}

func (o *oidcRuntime) exchange(ctx context.Context, code, verifier string) (string, error) {
	doc, err := o.discover(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", o.cfg.RedirectURI)
	form.Set("client_id", o.cfg.ClientID)
	form.Set("code_verifier", verifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(o.cfg.ClientID, o.cfg.ClientSecret)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", errors.New("oidc token rejected")
	}
	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.IDToken == "" {
		return "", errors.New("oidc token missing id_token")
	}
	return tok.IDToken, nil
}

type idClaims struct {
	Iss   string
	Sub   string
	Nonce string
	Exp   int64
	Aud   []string
}

func (o *oidcRuntime) verifyIDToken(ctx context.Context, raw, nonce string) (idClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return idClaims{}, errors.New("oidc token shape")
	}
	headRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return idClaims{}, err
	}
	var head struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headRaw, &head); err != nil {
		return idClaims{}, err
	}
	if head.Alg != "RS256" || head.Kid == "" {
		return idClaims{}, errors.New("oidc alg rejected")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return idClaims{}, err
	}
	keys, err := o.jwks(ctx)
	if err != nil {
		return idClaims{}, err
	}
	key := keys[head.Kid]
	if key == nil {
		return idClaims{}, errors.New("oidc key missing")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return idClaims{}, err
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return idClaims{}, err
	}
	var rawClaims struct {
		Iss   string          `json:"iss"`
		Sub   string          `json:"sub"`
		Aud   json.RawMessage `json:"aud"`
		Exp   int64           `json:"exp"`
		Iat   int64           `json:"iat"`
		Nonce string          `json:"nonce"`
	}
	if err := json.Unmarshal(payload, &rawClaims); err != nil {
		return idClaims{}, err
	}
	aud, err := parseAud(rawClaims.Aud)
	if err != nil {
		return idClaims{}, err
	}
	if rawClaims.Iss != o.cfg.Issuer || !containsStr(aud, o.cfg.ClientID) {
		return idClaims{}, errors.New("oidc iss or aud rejected")
	}
	if rawClaims.Sub == "" || rawClaims.Nonce == "" || rawClaims.Nonce != nonce {
		return idClaims{}, errors.New("oidc nonce rejected")
	}
	now := time.Now().Unix()
	if rawClaims.Exp <= now || rawClaims.Iat > now+60 {
		return idClaims{}, errors.New("oidc token expired")
	}
	return idClaims{Iss: rawClaims.Iss, Sub: rawClaims.Sub, Nonce: rawClaims.Nonce, Exp: rawClaims.Exp, Aud: aud}, nil
}

func parseAud(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("aud missing")
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil && one != "" {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil || len(many) == 0 {
		return nil, errors.New("aud rejected")
	}
	return many, nil
}

func containsStr(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func (o *oidcRuntime) jwks(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	o.mu.Lock()
	if o.keys != nil && time.Since(o.keysAt) < 5*time.Minute {
		keys := o.keys
		o.mu.Unlock()
		return keys, nil
	}
	o.mu.Unlock()
	doc, err := o.discover(ctx)
	if err != nil {
		return nil, err
	}
	body, err := o.get(ctx, doc.JWKSURI)
	if err != nil {
		return nil, err
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
			Alg string `json:"alg"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		nb, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eb, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		e := 0
		for _, b := range eb {
			e = e<<8 | int(b)
		}
		if e == 0 {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: e}
	}
	if len(keys) == 0 {
		return nil, errors.New("oidc jwks empty")
	}
	o.mu.Lock()
	o.keys = keys
	o.keysAt = time.Now()
	o.mu.Unlock()
	return keys, nil
}

func (o *oidcRuntime) get(ctx context.Context, raw string) ([]byte, error) {
	if _, err := endpointURL(o.cfg.Issuer, raw); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	res, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("oidc fetch failed")
	}
	return body, nil
}

func endpointURL(issuer, raw string) (*url.URL, error) {
	iss, err := url.Parse(issuer)
	if err != nil || iss.Host == "" || iss.User != nil {
		return nil, errors.New("oidc issuer rejected")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("oidc endpoint rejected")
	}
	if u.Host != iss.Host {
		return nil, errors.New("oidc endpoint host rejected")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback(u.Host) {
			return nil, errors.New("oidc endpoint scheme rejected")
		}
	default:
		return nil, errors.New("oidc endpoint scheme rejected")
	}
	return u, nil
}

func loopback(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func (s *Server) writeOIDCStateCookie(w http.ResponseWriter, state string) {
	name, secure := "goxio_oidc", false
	if s.portal != nil && s.portal.SecureCookies() {
		name, secure = "__Host-goxio_oidc", true
	}
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: state, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: 300,
	})
}

func (s *Server) readOIDCStateCookie(r *http.Request) string {
	name := "goxio_oidc"
	if s.portal != nil && s.portal.SecureCookies() {
		name = "__Host-goxio_oidc"
	}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Server) clearOIDCStateCookie(w http.ResponseWriter) {
	for _, name := range []string{"goxio_oidc", "__Host-goxio_oidc"} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", HttpOnly: true,
			Secure: name == "__Host-goxio_oidc", SameSite: http.SameSiteLaxMode, MaxAge: -1,
		})
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}
