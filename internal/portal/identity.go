package portal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// PolicyError is a rejected password or login shape. It is not a successful
// sign-in and it does not say which saved account was touched.
type PolicyError struct{ Msg string }

func (e *PolicyError) Error() string { return e.Msg }

func policy(msg string) error { return &PolicyError{Msg: msg} }

// LoginKey is the rate-limit identity. Email and slug case fold to one key.
func LoginKey(raw string) string {
	if canon, err := canonicalLogin(raw); err == nil {
		return canon
	}
	return strings.ToLower(strings.TrimSpace(raw))
}

// cleanCopiedPassword removes paste debris at the ends only.
// A byte-order mark, zero-width characters, and Unicode spaces at either end
// are not part of the password. Spaces inside the password stay.
func cleanCopiedPassword(raw string) string {
	s := strings.TrimPrefix(raw, "\uFEFF")
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff'
	})
}

// passwordCandidates is the stored secret, then the same secret without paste
// debris, when those differ. Internal spaces are never a second candidate.
func passwordCandidates(raw string) []string {
	if strings.ContainsRune(raw, 0) {
		return []string{raw}
	}
	cleaned := cleanCopiedPassword(raw)
	if cleaned == "" || cleaned == raw {
		return []string{raw}
	}
	return []string{raw, cleaned}
}

func preparePassword(raw string) (string, error) {
	if strings.ContainsRune(raw, 0) {
		return "", policy("password contains a null byte")
	}
	cleaned := cleanCopiedPassword(raw)
	if len(cleaned) < minPassword {
		return "", policy("password must be at least 12 characters")
	}
	if len(cleaned) > 72 {
		return "", policy("password must be at most 72 bytes")
	}
	return cleaned, nil
}

func passwordMatches(hash []byte, cands []string) bool {
	ok := false
	for _, c := range cands {
		if bcrypt.CompareHashAndPassword(hash, []byte(c)) == nil {
			ok = true
		}
	}
	return ok
}

func (s *Store) burn(cands []string) {
	for _, c := range cands {
		_ = bcrypt.CompareHashAndPassword(s.dummy, []byte(c))
	}
}

func (s *Store) withIdentLock(fn func() error) error {
	f, err := os.OpenFile(filepath.Join(s.dir, "accounts", ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn()
}

func accountMatches(acct Account, canon string) bool {
	if acct.Login == canon {
		return true
	}
	for _, alias := range acct.Aliases {
		if alias == canon {
			return true
		}
	}
	return false
}

// emailReservedBySlug reports that this exact address uses the local part of
// an existing slug. Self-service signup cannot take that address or join the
// slug's tenant. Dotted and +tag local parts are different slugs.
func (s *Store) emailReservedBySlug(canon string) bool {
	local, _, ok := strings.Cut(canon, "@")
	if !ok || !validSlug(local) {
		return false
	}
	_, err := s.accountByLogin(local)
	return err == nil
}

// slugForUnboundEmail finds the slug account whose login is this address's
// local part. The caller still checks the password. A dotted or +tag local
// part is a different slug, so it does not enter this account.
func (s *Store) slugForUnboundEmail(canon string) (Account, bool) {
	local, _, ok := strings.Cut(canon, "@")
	if !ok || !validSlug(local) {
		return Account{}, false
	}
	acct, err := s.accountByLogin(local)
	if err != nil || acct.Login != local {
		return Account{}, false
	}
	return acct, true
}

// Register creates one new account and a session for it.
// The account id is generated here. Login, password, and confirm are the only
// inputs. An address reserved by an existing slug is rejected and not attached.
func (s *Store) Register(login, password, confirm string) (Session, error) {
	login, err := canonicalLogin(login)
	if err != nil {
		return Session{}, err
	}
	password, err = preparePassword(password)
	if err != nil {
		return Session{}, err
	}
	if cleanCopiedPassword(confirm) != password {
		return Session{}, policy("passwords do not match")
	}
	if _, err := s.CreateAccount(login, password); err != nil {
		return Session{}, err
	}
	sess, err := s.Authenticate(login, password)
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			return Session{}, errSignInPending
		}
		return Session{}, err
	}
	return sess, nil
}

// errSignInPending means the account file was written and the session was not.
var errSignInPending = errors.New("account created; sign-in did not complete")

// SignInPending reports that register stored the account and did not open a session.
func SignInPending(err error) bool { return errors.Is(err, errSignInPending) }

func (s *Store) attachAlias(acct Account, alias string) error {
	fresh, err := s.readAccount(acct.ID)
	if err != nil {
		return err
	}
	for _, have := range fresh.Aliases {
		if have == alias || have == fresh.Login {
			if have == alias {
				return nil
			}
		}
	}
	if alias == fresh.Login {
		return nil
	}
	if other, err := s.accountByLogin(alias); err == nil && other.ID != fresh.ID {
		return ErrExists
	} else if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	fresh.Aliases = append(fresh.Aliases, alias)
	return writeJSON(filepath.Join(s.dir, "accounts", fresh.ID+".json"), fresh)
}

// normalizeAccounts rewrites mixed-case logins and aliases to their canonical
// form. Password hashes, account ids, and sessions are left as they are.
// Two records that collapse to the same name are not merged.
func (s *Store) normalizeAccounts() error {
	entries, err := os.ReadDir(filepath.Join(s.dir, "accounts"))
	if err != nil {
		return err
	}
	for _, e := range entries {
		acct, changed, ok := s.readCanonical(e.Name())
		if !ok || !changed {
			continue
		}
		if s.canonicalTaken(acct, e.Name()) {
			continue
		}
		if err := writeJSON(filepath.Join(s.dir, "accounts", e.Name()), acct); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) readCanonical(file string) (Account, bool, bool) {
	if !strings.HasSuffix(file, ".json") {
		return Account{}, false, false
	}
	var acct Account
	if err := readJSON(filepath.Join(s.dir, "accounts", file), &acct); err != nil {
		return Account{}, false, false
	}
	if !safeID(acct.ID) || file != acct.ID+".json" {
		return Account{}, false, false
	}
	login, err := canonicalLogin(acct.Login)
	if err != nil {
		return Account{}, false, false
	}
	changed := login != acct.Login
	acct.Login = login
	var aliases []string
	seen := map[string]bool{}
	for _, alias := range acct.Aliases {
		canon, err := canonicalLogin(alias)
		if err != nil || canon == acct.Login || seen[canon] {
			changed = true
			continue
		}
		seen[canon] = true
		if canon != alias {
			changed = true
		}
		aliases = append(aliases, canon)
	}
	if len(aliases) != len(acct.Aliases) {
		changed = true
	}
	if len(aliases) == 0 {
		acct.Aliases = nil
	} else {
		acct.Aliases = aliases
	}
	return acct, changed, true
}

func (s *Store) canonicalTaken(acct Account, file string) bool {
	entries, err := os.ReadDir(filepath.Join(s.dir, "accounts"))
	if err != nil {
		return true
	}
	names := append([]string{acct.Login}, acct.Aliases...)
	for _, e := range entries {
		if e.Name() == file || e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		other, _, ok := s.readCanonical(e.Name())
		if !ok || other.ID == acct.ID {
			continue
		}
		otherNames := append([]string{other.Login}, other.Aliases...)
		for _, a := range names {
			for _, b := range otherNames {
				if a == b {
					return true
				}
			}
		}
	}
	return false
}
