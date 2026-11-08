package portal

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxCompanyName = 80

// AccountProfile is the client identity shown on the servers page.
// It never includes a password hash.
type AccountProfile struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	Email       string `json:"email"`
	CompanyName string `json:"company_name"`
	CreatedAt   string `json:"created_at"`
	ServerCount int    `json:"server_count"`
}

func prepareCompanyName(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", policy("invalid company name")
	}
	raw = strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if utf8.RuneCountInString(raw) > maxCompanyName {
		return "", policy("company name must be at most 80 characters")
	}
	return raw, nil
}

func accountEmail(acct Account) string {
	for _, alias := range acct.Aliases {
		if strings.Contains(alias, "@") {
			return alias
		}
	}
	if strings.Contains(acct.Login, "@") {
		return acct.Login
	}
	return ""
}

func (s *Store) saveAccount(acct Account) error {
	return writeJSON(filepath.Join(s.dir, "accounts", acct.ID+".json"), acct)
}

func (s *Store) countServersLocked(accountID string) int {
	dir := filepath.Join(s.dir, "servers", accountID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var srv Server
		if err := readJSON(filepath.Join(dir, e.Name()), &srv); err != nil {
			continue
		}
		if srv.AccountID == accountID {
			n++
		}
	}
	return n
}

func (s *Store) profileLocked(acct Account) AccountProfile {
	return AccountProfile{
		ID:          acct.ID,
		Login:       acct.Login,
		Email:       accountEmail(acct),
		CompanyName: acct.CompanyName,
		CreatedAt:   acct.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		ServerCount: s.countServersLocked(acct.ID),
	}
}

// Profile returns the public client record for the signed-in account.
func (s *Store) Profile(accountID string) (AccountProfile, error) {
	if !safeID(accountID) {
		return AccountProfile{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, err := s.readAccount(accountID)
	if err != nil {
		return AccountProfile{}, ErrNotFound
	}
	return s.profileLocked(acct), nil
}

// SetCompanyName stores the display name shown at the top of the servers page.
func (s *Store) SetCompanyName(accountID, raw string) (AccountProfile, error) {
	name, err := prepareCompanyName(raw)
	if err != nil {
		return AccountProfile{}, err
	}
	if !safeID(accountID) {
		return AccountProfile{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, err := s.readAccount(accountID)
	if err != nil {
		return AccountProfile{}, ErrNotFound
	}
	acct.CompanyName = name
	if err := s.saveAccount(acct); err != nil {
		return AccountProfile{}, err
	}
	return s.profileLocked(acct), nil
}
