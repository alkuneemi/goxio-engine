// Package portal is the GoXIO sign-in and tenant boundary.
//
// A session is an HttpOnly cookie that names a server-side record. Every map
// is loaded from that account's own server snapshot. The process never SSHes
// to a customer host and never returns ingest tokens or password hashes.
package portal

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

const (
	defaultTTL  = 12 * time.Hour
	defaultCost = 12
	minPassword = 12
	maxBody     = 8 << 20
)

var (
	ErrExists   = errors.New("already exists")
	ErrInvalid  = errors.New("invalid login or password")
	ErrNotFound = errors.New("not found")
	ErrExpired  = errors.New("session expired")
	ErrRate     = errors.New("too many attempts")
	ErrCSRF     = errors.New("csrf check failed")
	ErrToken    = errors.New("invalid ingest token")
)

type Options struct {
	SecureCookies bool
	TrustProxy    bool
	BcryptCost    int
	SessionTTL    time.Duration
	LoginMax      int
	LoginWindow   time.Duration
}

type Account struct {
	ID           string    `json:"id"`
	Login        string    `json:"login"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

// Server is stored on disk. IngestHash is never copied into ServerView.
type Server struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"account_id"`
	Name       string    `json:"name"`
	Address    string    `json:"address"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Status     string    `json:"status"`
	IngestHash string    `json:"ingest_hash"`
	Partial    bool      `json:"partial"`
	Complete   bool      `json:"complete"`
	Detail     string    `json:"detail,omitempty"`
}

// ServerView is the only server shape returned to a browser.
type ServerView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Status    string    `json:"status"`
	Partial   bool      `json:"partial"`
	Complete  bool      `json:"complete"`
	Detail    string    `json:"detail,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type Session struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	CSRF      string    `json:"csrf"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked"`
}

type Receipt struct {
	ID        string    `json:"id"`
	AccountID string    `json:"account_id"`
	ServerID  string    `json:"server_id"`
	At        time.Time `json:"at"`
	Status    string    `json:"status"`
}

// Meta describes a tenant map without borrowing another tenant's graph.
type Meta struct {
	Status           string `json:"status"`
	Partial          bool   `json:"partial"`
	Complete         bool   `json:"complete"`
	Detail           string `json:"detail,omitempty"`
	Host             string `json:"host,omitempty"`
	MissingSizeCount int    `json:"missing_size_count"`
	TruncatedCount   int    `json:"truncated_count"`
	ListedContainers int    `json:"listed_containers"`
}

type Store struct {
	dir    string
	pepper []byte
	opt    Options
	dummy  []byte
	mu     sync.Mutex
	byHash map[string]serverKey
	fails  map[string][]time.Time
}

type serverKey struct {
	accountID string
	serverID  string
}

func Open(dir string, opt Options) (*Store, error) {
	if opt.BcryptCost == 0 {
		opt.BcryptCost = defaultCost
	}
	if opt.BcryptCost < bcrypt.MinCost {
		opt.BcryptCost = bcrypt.MinCost
	}
	if opt.SessionTTL <= 0 {
		opt.SessionTTL = defaultTTL
	}
	if opt.LoginMax <= 0 {
		opt.LoginMax = 8
	}
	if opt.LoginWindow <= 0 {
		opt.LoginWindow = 15 * time.Minute
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	for _, sub := range []string{"accounts", "sessions", "servers", "snapshots", "receipts"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	pepper, err := loadPepper(filepath.Join(dir, "pepper"))
	if err != nil {
		return nil, err
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("goxio-not-a-real-password"), opt.BcryptCost)
	if err != nil {
		return nil, err
	}
	s := &Store{
		dir:    dir,
		pepper: pepper,
		opt:    opt,
		dummy:  dummy,
		byHash: map[string]serverKey{},
		fails:  map[string][]time.Time{},
	}
	if err := s.reindex(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) SecureCookies() bool { return s.opt.SecureCookies }
func (s *Store) TrustProxy() bool    { return s.opt.TrustProxy }

func (s *Store) CreateAccount(login, password string) (Account, error) {
	login = normalizeLogin(login)
	if !validLogin(login) {
		return Account{}, errors.New("login must be 2–32 lowercase letters, digits, dot, underscore or hyphen")
	}
	if len(password) < minPassword {
		return Account{}, errors.New("password must be at least 12 characters")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.accountByLogin(login); err == nil {
		return Account{}, ErrExists
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.opt.BcryptCost)
	if err != nil {
		return Account{}, err
	}
	acct := Account{
		ID:           "acct_" + randHex(16),
		Login:        login,
		PasswordHash: string(hash),
		CreatedAt:    time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(s.dir, "accounts", acct.ID+".json"), acct); err != nil {
		return Account{}, err
	}
	return acct, nil
}

// AddServer records a server owned by accountID and returns a fresh ingest
// token. The token is not stored and is not written to the logger.
func (s *Store) AddServer(accountID, name, address string) (Server, string, error) {
	if !safeID(accountID) {
		return Server{}, "", ErrNotFound
	}
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	if name == "" || len(name) > 80 || strings.ContainsAny(name, "\r\n") {
		return Server{}, "", errors.New("server name is required")
	}
	if len(address) > 128 || strings.ContainsAny(address, "\r\n\t ") || strings.Contains(address, "://") {
		return Server{}, "", errors.New("address must be a host or IP without credentials")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readAccount(accountID); err != nil {
		return Server{}, "", ErrNotFound
	}
	raw := randHex(32)
	sum := tokenHash(s.pepper, raw)
	srv := Server{
		ID:         "srv_" + randHex(16),
		AccountID:  accountID,
		Name:       name,
		Address:    address,
		CreatedAt:  time.Now().UTC(),
		Status:     "pending",
		IngestHash: sum,
		Detail:     "Map is being prepared. No inventory has been received for this server.",
	}
	if err := s.saveServer(srv); err != nil {
		return Server{}, "", err
	}
	s.byHash[sum] = serverKey{accountID: accountID, serverID: srv.ID}
	return srv, raw, nil
}

func (s *Store) Authenticate(login, password string) (Session, error) {
	login = normalizeLogin(login)
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, err := s.accountByLogin(login)
	hash := s.dummy
	if err == nil {
		hash = []byte(acct.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || err != nil {
		return Session{}, ErrInvalid
	}
	sess := Session{
		ID:        randHex(32),
		AccountID: acct.ID,
		CSRF:      randHex(32),
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(s.opt.SessionTTL),
	}
	if err := writeJSON(filepath.Join(s.dir, "sessions", sess.ID+".json"), sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

func (s *Store) LookupSession(id string) (Session, error) {
	if !safeID(id) || len(id) < 32 {
		return Session{}, ErrExpired
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var sess Session
	if err := readJSON(filepath.Join(s.dir, "sessions", id+".json"), &sess); err != nil {
		return Session{}, ErrExpired
	}
	if sess.Revoked || time.Now().After(sess.ExpiresAt) || sess.ID != id {
		return Session{}, ErrExpired
	}
	return sess, nil
}

func (s *Store) Revoke(id string) {
	if !safeID(id) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := filepath.Join(s.dir, "sessions", id+".json")
	var sess Session
	if err := readJSON(path, &sess); err != nil {
		return
	}
	sess.Revoked = true
	_ = writeJSON(path, sess)
}

func (s *Store) CheckCSRF(sess Session, header string) error {
	if len(sess.CSRF) == 0 || len(header) != len(sess.CSRF) || subtle.ConstantTimeCompare([]byte(sess.CSRF), []byte(header)) != 1 {
		return ErrCSRF
	}
	return nil
}

func (s *Store) AllowLogin(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneFails(key)
	if len(s.fails[key]) >= s.opt.LoginMax {
		return ErrRate
	}
	return nil
}

func (s *Store) NoteLoginFail(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneFails(key)
	s.fails[key] = append(s.fails[key], time.Now())
}

func (s *Store) ClearLoginFail(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.fails, key)
}

func (s *Store) ListServers(accountID string) ([]ServerView, error) {
	if !safeID(accountID) {
		return nil, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.dir, "servers", accountID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ServerView{}, nil
		}
		return nil, err
	}
	out := make([]ServerView, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var srv Server
		if err := readJSON(filepath.Join(dir, e.Name()), &srv); err != nil {
			continue
		}
		if srv.AccountID != accountID {
			continue
		}
		out = append(out, srv.view())
	}
	return out, nil
}

// FindAccount returns the account record. Callers must not serialize PasswordHash.
func (s *Store) FindAccount(login string) (Account, error) {
	login = normalizeLogin(login)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.accountByLogin(login)
}

func (s *Store) LookupServer(accountID, serverID string) (Server, error) {
	if !safeID(accountID) || !safeID(serverID) {
		return Server{}, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	srv, err := s.readServer(accountID, serverID)
	if err != nil || srv.AccountID != accountID {
		return Server{}, ErrNotFound
	}
	return srv, nil
}

func (s *Store) SaveSnapshot(accountID, serverID string, snap space.Snapshot) error {
	if !safeID(accountID) || !safeID(serverID) {
		return ErrNotFound
	}
	snap = space.Sanitize(snap)
	s.mu.Lock()
	defer s.mu.Unlock()
	srv, err := s.readServer(accountID, serverID)
	if err != nil || srv.AccountID != accountID {
		return ErrNotFound
	}
	if err := writeJSON(s.snapPath(accountID, serverID), snap); err != nil {
		return err
	}
	srv.UpdatedAt = time.Now().UTC()
	srv.Partial = snap.Partial
	srv.Complete = snap.Complete
	srv.Detail = snap.Detail
	if len(snap.Nodes) == 0 {
		srv.Status = "pending"
		if srv.Detail == "" {
			srv.Detail = "Map is being prepared. No inventory has been received for this server."
		}
	} else {
		srv.Status = "ready"
	}
	return s.saveServer(srv)
}

func (s *Store) LoadSnapshot(accountID, serverID string) (space.Snapshot, bool, error) {
	if !safeID(accountID) || !safeID(serverID) {
		return space.Snapshot{}, false, ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readServer(accountID, serverID); err != nil {
		return space.Snapshot{}, false, ErrNotFound
	}
	var snap space.Snapshot
	err := readJSON(s.snapPath(accountID, serverID), &snap)
	if err != nil {
		if os.IsNotExist(err) {
			return space.Snapshot{}, false, nil
		}
		return space.Snapshot{}, false, err
	}
	return snap, true, nil
}

func (s *Store) Graph(accountID, serverID string) (*graph.Graph, Meta, error) {
	snap, ok, err := s.LoadSnapshot(accountID, serverID)
	if err != nil {
		return nil, Meta{}, err
	}
	if !ok || len(snap.Nodes) == 0 {
		return emptyGraph(), Meta{
			Status:   "pending",
			Complete: false,
			Detail:   "Map is being prepared. No inventory has been received for this server.",
		}, nil
	}
	return snap.Graph(), Meta{
		Status:           "ready",
		Partial:          snap.Partial,
		Complete:         snap.Complete,
		Detail:           snap.Detail,
		Host:             snap.Host,
		MissingSizeCount: snap.MissingSizeCount,
		TruncatedCount:   snap.TruncatedCount,
		ListedContainers: snap.ListedContainers,
	}, nil
}

func (s *Store) Apply(accountID, serverID string, req space.ActionRequest) (space.ActionRecord, error) {
	snap, ok, err := s.LoadSnapshot(accountID, serverID)
	if err != nil {
		return space.ActionRecord{}, err
	}
	if !ok {
		return space.ActionRecord{}, ErrNotFound
	}
	st := space.NewStore(nil)
	st.Load(snap)
	rec, err := st.Apply(req)
	if err != nil {
		return space.ActionRecord{}, err
	}
	g := st.Graph()
	snap.Nodes = g.Nodes
	snap.Edges = g.Edges
	if err := s.SaveSnapshot(accountID, serverID, snap); err != nil {
		return space.ActionRecord{}, err
	}
	return rec, nil
}

// Ingest binds the body to the server named by the token. Client-supplied
// account or server ids are ignored. The raw token is never logged.
func (s *Store) Ingest(token string, body []byte) (Meta, error) {
	if len(body) > maxBody {
		return Meta{}, errors.New("snapshot too large")
	}
	sum := tokenHash(s.pepper, token)
	s.mu.Lock()
	key, ok := s.byHash[sum]
	s.mu.Unlock()
	if !ok || token == "" {
		return Meta{}, ErrToken
	}
	var snap space.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return Meta{}, errors.New("invalid snapshot json")
	}
	snap.Source = "ingest"
	if err := s.SaveSnapshot(key.accountID, key.serverID, snap); err != nil {
		return Meta{}, err
	}
	_, meta, err := s.Graph(key.accountID, key.serverID)
	return meta, err
}

func (s *Store) RequestRefresh(accountID, serverID string) (Receipt, error) {
	if !safeID(accountID) || !safeID(serverID) {
		return Receipt{}, ErrNotFound
	}
	rec := Receipt{
		ID:        "rcpt_" + randHex(12),
		AccountID: accountID,
		ServerID:  serverID,
		At:        time.Now().UTC(),
		Status:    "pending",
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.readServer(accountID, serverID); err != nil {
		return Receipt{}, ErrNotFound
	}
	dir := filepath.Join(s.dir, "receipts", accountID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Receipt{}, err
	}
	if err := writeJSON(filepath.Join(dir, rec.ID+".json"), rec); err != nil {
		return Receipt{}, err
	}
	return rec, nil
}

func (s *Store) pruneFails(key string) {
	cut := time.Now().Add(-s.opt.LoginWindow)
	kept := s.fails[key][:0]
	for _, ts := range s.fails[key] {
		if ts.After(cut) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		delete(s.fails, key)
		return
	}
	s.fails[key] = kept
}

func (srv Server) view() ServerView {
	return ServerView{
		ID: srv.ID, Name: srv.Name, Address: srv.Address, Status: srv.Status,
		Partial: srv.Partial, Complete: srv.Complete, Detail: srv.Detail, UpdatedAt: srv.UpdatedAt,
	}
}

func (s *Store) saveServer(srv Server) error {
	dir := filepath.Join(s.dir, "servers", srv.AccountID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, srv.ID+".json"), srv)
}

func (s *Store) readServer(accountID, serverID string) (Server, error) {
	var srv Server
	err := readJSON(filepath.Join(s.dir, "servers", accountID, serverID+".json"), &srv)
	if err != nil {
		return Server{}, err
	}
	return srv, nil
}

func (s *Store) readAccount(id string) (Account, error) {
	var acct Account
	if err := readJSON(filepath.Join(s.dir, "accounts", id+".json"), &acct); err != nil {
		return Account{}, err
	}
	return acct, nil
}

func (s *Store) accountByLogin(login string) (Account, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "accounts"))
	if err != nil {
		return Account{}, err
	}
	for _, e := range entries {
		var acct Account
		if err := readJSON(filepath.Join(s.dir, "accounts", e.Name()), &acct); err != nil {
			continue
		}
		if acct.Login == login {
			return acct, nil
		}
	}
	return Account{}, ErrNotFound
}

func (s *Store) reindex() error {
	root := filepath.Join(s.dir, "servers")
	accounts, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, acct := range accounts {
		if !acct.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, acct.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			var srv Server
			if err := readJSON(filepath.Join(root, acct.Name(), f.Name()), &srv); err != nil {
				continue
			}
			if srv.IngestHash != "" && srv.AccountID == acct.Name() {
				s.byHash[srv.IngestHash] = serverKey{accountID: srv.AccountID, serverID: srv.ID}
			}
		}
	}
	return nil
}

func (s *Store) snapPath(accountID, serverID string) string {
	return filepath.Join(s.dir, "snapshots", accountID, serverID+".json")
}

func emptyGraph() *graph.Graph {
	return &graph.Graph{Nodes: []nodes.Node{}, Edges: []edges.Edge{}}
}

func loadPepper(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err == nil && len(bytesTrim(b)) >= 32 {
		return bytesTrim(b), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	raw := randHex(32)
	if err := os.WriteFile(path, []byte(raw+"\n"), 0o600); err != nil {
		return nil, err
	}
	return []byte(raw), nil
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func tokenHash(pepper []byte, token string) string {
	h := sha256.New()
	_, _ = h.Write(pepper)
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func normalizeLogin(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validLogin(s string) bool {
	if len(s) < 2 || len(s) > 32 {
		return false
	}
	for i, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok || (i == 0 && (r < 'a' || r > 'z') && (r < '0' || r > '9')) {
			return false
		}
	}
	return true
}

// SafeID reports whether id can be used as a single path segment we generated.
func SafeID(id string) bool { return safeID(id) }

func safeID(id string) bool {
	if id == "" || len(id) > 80 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
