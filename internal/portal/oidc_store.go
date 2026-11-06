package portal

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// OIDCPending is one in-flight authorization-code attempt.
// The verifier never leaves the server.
type OIDCPending struct {
	State     string    `json:"state"`
	Verifier  string    `json:"verifier"`
	Nonce     string    `json:"nonce"`
	AccountID string    `json:"account_id,omitempty"`
	Expires   time.Time `json:"expires"`
}

type oidcSubjectFile struct {
	Issuer    string `json:"issuer"`
	Subject   string `json:"subject"`
	AccountID string `json:"account_id"`
}

func oidcSubjectName(issuer, subject string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(issuer))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(subject))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *Store) PutOIDCPending(p OIDCPending) error {
	if p.State == "" || len(p.State) > 128 || !safeToken(p.State) {
		return errors.New("oidc state rejected")
	}
	return writeJSON(filepath.Join(s.dir, "oidc-pending", p.State+".json"), p)
}

// ConsumeOIDCPending returns the attempt once and deletes it.
func (s *Store) ConsumeOIDCPending(state string) (OIDCPending, error) {
	if !safeToken(state) {
		return OIDCPending{}, ErrNotFound
	}
	path := filepath.Join(s.dir, "oidc-pending", state+".json")
	var p OIDCPending
	if err := readJSON(path, &p); err != nil {
		return OIDCPending{}, ErrNotFound
	}
	_ = os.Remove(path)
	if p.State != state || time.Now().After(p.Expires) {
		return OIDCPending{}, ErrNotFound
	}
	return p, nil
}

func safeToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// FindOIDCAccount returns the account bound to this issuer and subject.
// Email is not consulted.
func (s *Store) FindOIDCAccount(issuer, subject string) (Account, error) {
	if issuer == "" || subject == "" {
		return Account{}, ErrNotFound
	}
	var rec oidcSubjectFile
	path := filepath.Join(s.dir, "oidc-subjects", oidcSubjectName(issuer, subject)+".json")
	if err := readJSON(path, &rec); err != nil {
		return Account{}, ErrNotFound
	}
	if rec.Issuer != issuer || rec.Subject != subject || !safeID(rec.AccountID) {
		return Account{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, err := s.readAccount(rec.AccountID)
	if err != nil || acct.OIDCIssuer != issuer || acct.OIDCSubject != subject {
		return Account{}, ErrNotFound
	}
	return acct, nil
}

// BindOIDC attaches issuer+subject to an existing account.
// A subject already bound to a different account is rejected and not moved.
func (s *Store) BindOIDC(accountID, issuer, subject string) error {
	if !safeID(accountID) || issuer == "" || subject == "" {
		return ErrNotFound
	}
	return s.withIdentLock(func() error {
		acct, err := s.readAccount(accountID)
		if err != nil {
			return ErrNotFound
		}
		if acct.OIDCIssuer != "" && (acct.OIDCIssuer != issuer || acct.OIDCSubject != subject) {
			return ErrExists
		}
		path := filepath.Join(s.dir, "oidc-subjects", oidcSubjectName(issuer, subject)+".json")
		if _, err := os.Stat(path); err == nil {
			var rec oidcSubjectFile
			if err := readJSON(path, &rec); err != nil {
				return err
			}
			if rec.AccountID != accountID {
				return ErrExists
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		rec := oidcSubjectFile{Issuer: issuer, Subject: subject, AccountID: accountID}
		if err := writeJSON(path, rec); err != nil {
			return err
		}
		acct.OIDCIssuer = issuer
		acct.OIDCSubject = subject
		return writeJSON(filepath.Join(s.dir, "accounts", acct.ID+".json"), acct)
	})
}

// CreateSSOAccount creates an empty tenant for a new issuer+subject.
// The password is random and is not returned. The login is not the email.
func (s *Store) CreateSSOAccount(issuer, subject string) (Account, error) {
	if issuer == "" || subject == "" {
		return Account{}, ErrNotFound
	}
	secret := randHex(32)
	login := "sso" + randHex(6)
	acct, err := s.CreateAccount(login, secret)
	if err != nil {
		return Account{}, err
	}
	if err := s.BindOIDC(acct.ID, issuer, subject); err != nil {
		return Account{}, err
	}
	acct.OIDCIssuer = issuer
	acct.OIDCSubject = subject
	return acct, nil
}

// IssueSession opens a server-side session for an account that has already
// been authenticated. It does not check a password.
func (s *Store) IssueSession(accountID string) (Session, error) {
	if !safeID(accountID) {
		return Session{}, ErrNotFound
	}
	var sess Session
	err := s.withIdentLock(func() error {
		if _, err := s.readAccount(accountID); err != nil {
			return ErrNotFound
		}
		sess = Session{
			ID:        randHex(32),
			AccountID: accountID,
			CSRF:      randHex(32),
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(s.opt.SessionTTL),
		}
		return writeJSON(filepath.Join(s.dir, "sessions", sess.ID+".json"), sess)
	})
	return sess, err
}
