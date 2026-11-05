package space

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/redact"
	"go.uber.org/zap"
)

// Summary is the public description of the current snapshot.
type Summary struct {
	Source      string    `json:"source"`
	Host        string    `json:"host"`
	CollectedAt time.Time `json:"collected_at"`
	HostWrites  bool      `json:"host_writes"`
	Detail      string    `json:"detail,omitempty"`
	NodeCount   int       `json:"node_count"`
	EdgeCount   int       `json:"edge_count"`
}

// ActionRequest is a confirmed space action. Confirm must be the exact word
// DELETE, ARCHIVE, or TRANSFER. Nothing in this request is applied to the host.
type ActionRequest struct {
	NodeID        string `json:"node_id"`
	Action        string `json:"action"`
	Confirm       string `json:"confirm"`
	TargetAccount string `json:"target_account,omitempty"`
	TargetServer  string `json:"target_server,omitempty"`
}

// ActionRecord is an audit row. AppliedToHost is always false.
type ActionRecord struct {
	ID            string    `json:"id"`
	At            time.Time `json:"at"`
	NodeID        string    `json:"node_id"`
	NodeName      string    `json:"node_name"`
	Action        string    `json:"action"`
	TargetAccount string    `json:"target_account,omitempty"`
	TargetServer  string    `json:"target_server,omitempty"`
	AppliedToHost bool      `json:"applied_to_host"`
	Note          string    `json:"note"`
}

// StatusError is an action or ingest failure with an HTTP status.
type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return e.Msg }

type overlay struct {
	State         string
	Archived      bool
	PendingDelete bool
	GitHubAccount string
	Server        string
	RetargetOwner string
	Note          string
}

// Store holds the latest snapshot, in-memory action overlays, and the audit log.
type Store struct {
	mu          sync.RWMutex
	logger      *zap.SugaredLogger
	snap        Snapshot
	overlays    map[string]overlay
	extraNodes  []nodes.Node
	actions     []ActionRecord
	ingestToken string
}

// NewStore returns an empty store. Host writes stay off.
func NewStore(logger *zap.SugaredLogger) *Store {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	return &Store{
		logger:   logger,
		overlays: map[string]overlay{},
		snap: Snapshot{
			Source:     "",
			HostWrites: false,
			Nodes:      []nodes.Node{},
			Edges:      []edges.Edge{},
		},
	}
}

// SetIngestToken requires this token on POST /api/space/ingest when non-empty.
func (s *Store) SetIngestToken(token string) {
	s.mu.Lock()
	s.ingestToken = strings.TrimSpace(token)
	s.mu.Unlock()
}

// ModeFromEnv reads GOXIO_SPACE_SOURCE. Unknown values behave as auto.
func ModeFromEnv() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOXIO_SPACE_SOURCE"))) {
	case "demo", "host", "auto":
		return strings.ToLower(strings.TrimSpace(os.Getenv("GOXIO_SPACE_SOURCE")))
	default:
		return "auto"
	}
}

// Load replaces the snapshot and keeps action overlays so a refresh does not
// forget a confirmed request. The snapshot is marked read-only.
func (s *Store) Load(snap Snapshot) {
	snap = sanitize(snap)
	s.mu.Lock()
	s.snap = snap
	s.mu.Unlock()
}

// Source is host, demo, ingest, or empty.
func (s *Store) Source() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap.Source
}

// Summary describes the current snapshot.
func (s *Store) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g := s.graphLocked()
	return Summary{
		Source:      s.snap.Source,
		Host:        s.snap.Host,
		CollectedAt: s.snap.CollectedAt,
		HostWrites:  false,
		Detail:      s.snap.Detail,
		NodeCount:   len(g.Nodes),
		EdgeCount:   len(g.Edges),
	}
}

// Graph is the space graph with action overlays applied.
func (s *Store) Graph() *graph.Graph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.graphLocked()
}

// Merge appends space nodes and edges onto base. Existing IDs win so discovery
// nodes are not replaced. base is not mutated.
func (s *Store) Merge(base *graph.Graph) *graph.Graph {
	sg := s.Graph()
	if base == nil {
		return sg
	}
	nodesOut := make([]nodes.Node, 0, len(base.Nodes)+len(sg.Nodes))
	seen := make(map[string]bool, len(base.Nodes)+len(sg.Nodes))
	for _, n := range base.Nodes {
		if n.Id == "" || seen[n.Id] {
			continue
		}
		seen[n.Id] = true
		nodesOut = append(nodesOut, n)
	}
	for _, n := range sg.Nodes {
		if n.Id == "" || seen[n.Id] {
			continue
		}
		seen[n.Id] = true
		nodesOut = append(nodesOut, n)
	}
	edgesOut := append([]edges.Edge{}, base.Edges...)
	if edgesOut == nil {
		edgesOut = []edges.Edge{}
	}
	eseen := make(map[string]bool, len(edgesOut))
	for _, e := range edgesOut {
		eseen[e.Id] = true
	}
	for _, e := range sg.Edges {
		if eseen[e.Id] || !seen[e.Source] || !seen[e.Target] {
			continue
		}
		eseen[e.Id] = true
		edgesOut = append(edgesOut, e)
	}
	return &graph.Graph{Nodes: nodesOut, Edges: edgesOut}
}

// Actions returns a copy of the audit log, oldest first.
func (s *Store) Actions() []ActionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]ActionRecord{}, s.actions...)
	if out == nil {
		out = []ActionRecord{}
	}
	return out
}

// Apply records a confirmed action. It updates the in-memory map only.
func (s *Store) Apply(req ActionRequest) (ActionRecord, error) {
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.TargetAccount = strings.TrimSpace(req.TargetAccount)
	req.TargetServer = strings.TrimSpace(req.TargetServer)
	expect, ok := confirmWord(req.Action)
	if !ok {
		return ActionRecord{}, &StatusError{Code: 400, Msg: "action must be delete, archive, or transfer"}
	}
	if req.Confirm != expect {
		return ActionRecord{}, &StatusError{Code: 400, Msg: "type " + expect + " to confirm — nothing was changed"}
	}
	if req.Action == "transfer" && req.TargetAccount == "" && req.TargetServer == "" {
		return ActionRecord{}, &StatusError{Code: 400, Msg: "transfer needs a GitHub account or a server"}
	}
	if msg := rejectTarget(req.TargetAccount); msg != "" {
		return ActionRecord{}, &StatusError{Code: 400, Msg: msg}
	}
	if msg := rejectTarget(req.TargetServer); msg != "" {
		return ActionRecord{}, &StatusError{Code: 400, Msg: msg}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	node, found := s.findLocked(req.NodeID)
	if !found {
		return ActionRecord{}, &StatusError{Code: 404, Msg: "space node not found"}
	}
	if msg := protected(node, req.Action); msg != "" {
		return ActionRecord{}, &StatusError{Code: 409, Msg: msg}
	}

	ov := s.overlays[node.Id]
	ov.Note = "Recorded only. GoXIO did not modify the host."
	switch req.Action {
	case "delete":
		ov.PendingDelete = true
		ov.State = "pending_delete"
	case "archive":
		ov.Archived = true
		ov.State = "archived"
	case "transfer":
		if req.TargetAccount != "" {
			ov.GitHubAccount = req.TargetAccount
			if node.Type == string(nodes.TypeRepo) {
				ov.RetargetOwner = s.ensureAccountLocked(req.TargetAccount)
			}
		}
		if req.TargetServer != "" {
			ov.Server = req.TargetServer
		}
		if ov.State == "" {
			ov.State = "transferred"
		}
	}
	s.overlays[node.Id] = ov

	rec := ActionRecord{
		ID:            time.Now().UTC().Format("20060102T150405.000000000") + "-" + req.Action,
		At:            time.Now().UTC(),
		NodeID:        node.Id,
		NodeName:      node.Name,
		Action:        req.Action,
		TargetAccount: req.TargetAccount,
		TargetServer:  req.TargetServer,
		AppliedToHost: false,
		Note:          ov.Note,
	}
	s.actions = append(s.actions, rec)
	if len(s.actions) > 200 {
		s.actions = s.actions[len(s.actions)-200:]
	}
	return rec, nil
}

// Ingest replaces the snapshot with a posted collector payload. Secrets in
// metadata are masked. The host is not contacted.
func (s *Store) Ingest(r io.Reader, token string) error {
	s.mu.RLock()
	want := s.ingestToken
	s.mu.RUnlock()
	if want != "" && token != want {
		return &StatusError{Code: 401, Msg: "ingest token rejected"}
	}
	dec := json.NewDecoder(io.LimitReader(r, 2<<20))
	var snap Snapshot
	if err := dec.Decode(&snap); err != nil {
		return &StatusError{Code: 400, Msg: "invalid snapshot json"}
	}
	snap.Source = sourceIngest
	snap = sanitize(snap)
	if len(snap.Nodes) == 0 {
		return &StatusError{Code: 400, Msg: "snapshot has no space nodes"}
	}
	if len(snap.Nodes) > 5000 {
		return &StatusError{Code: 400, Msg: "snapshot too large"}
	}
	s.Load(snap)
	return nil
}

// Bootstrap fills the store once. auto uses the host when it has space and
// otherwise the demo snapshot.
func (s *Store) Bootstrap(ctx context.Context, mode string) {
	switch mode {
	case "demo":
		s.Load(Demo(time.Now()))
		s.logger.Infow("space map using demo snapshot")
	case "host":
		snap, err := Collect(ctx, s.logger)
		if err != nil {
			s.logger.Warnw("space host collect failed", "err", err)
		}
		if snap.CollectedAt.IsZero() {
			snap.CollectedAt = time.Now().UTC()
		}
		if snap.Source == "" {
			snap.Source = sourceHost
		}
		s.Load(snap)
	default:
		snap, err := Collect(ctx, s.logger)
		if err != nil || !snap.HasSpace() {
			s.logger.Infow("space collector empty, using demo snapshot", "err", err)
			s.Load(Demo(time.Now()))
			return
		}
		s.Load(snap)
		s.logger.Infow("space map collected from host", "nodes", len(snap.Nodes))
	}
}

// Loop refreshes a host snapshot. Demo and ingest snapshots are left in place
// unless mode is host.
func (s *Store) Loop(ctx context.Context, mode string) {
	if mode == "demo" {
		return
	}
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if mode != "host" && s.Source() != sourceHost {
				continue
			}
			cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
			snap, err := Collect(cctx, s.logger)
			cancel()
			if err != nil || !snap.HasSpace() {
				s.logger.Warnw("space refresh skipped", "err", err)
				continue
			}
			s.Load(snap)
		}
	}
}

func (s *Store) graphLocked() *graph.Graph {
	byID := map[string]nodes.Node{}
	var order []string
	add := func(n nodes.Node) {
		n = applyOverlay(n, s.overlays[n.Id])
		if _, ok := byID[n.Id]; ok {
			byID[n.Id] = n
			return
		}
		byID[n.Id] = n
		order = append(order, n.Id)
	}
	for _, n := range s.snap.Nodes {
		add(n)
	}
	for _, n := range s.extraNodes {
		add(n)
	}
	nodesOut := make([]nodes.Node, 0, len(order))
	for _, id := range order {
		nodesOut = append(nodesOut, byID[id])
	}

	retarget := map[string]string{}
	for id, ov := range s.overlays {
		if ov.RetargetOwner != "" {
			retarget[id] = ov.RetargetOwner
		}
	}
	var edgesOut []edges.Edge
	seen := map[string]bool{}
	push := func(e edges.Edge) {
		if e.Source == "" || e.Target == "" || seen[e.Id] {
			return
		}
		if _, ok := byID[e.Source]; !ok {
			return
		}
		if _, ok := byID[e.Target]; !ok {
			return
		}
		seen[e.Id] = true
		edgesOut = append(edgesOut, e)
	}
	for _, e := range s.snap.Edges {
		if e.Type == edges.TypeOwns {
			if _, ok := retarget[e.Target]; ok {
				continue
			}
		}
		push(e)
	}
	for repo, acct := range retarget {
		push(newEdge(acct, repo, edges.TypeOwns, "OWNS"))
	}
	if nodesOut == nil {
		nodesOut = []nodes.Node{}
	}
	if edgesOut == nil {
		edgesOut = []edges.Edge{}
	}
	return &graph.Graph{Nodes: nodesOut, Edges: edgesOut}
}

func applyOverlay(n nodes.Node, ov overlay) nodes.Node {
	if ov == (overlay{}) {
		return n
	}
	if n.Metadata == nil {
		n.Metadata = map[string]any{}
	}
	md := make(map[string]any, len(n.Metadata)+4)
	for k, v := range n.Metadata {
		md[k] = v
	}
	if ov.State != "" {
		md["state"] = ov.State
	}
	if ov.Archived {
		md["archived"] = true
	}
	if ov.PendingDelete {
		md["pending_delete"] = true
	}
	if ov.GitHubAccount != "" {
		md["github_account"] = ov.GitHubAccount
	}
	if ov.Server != "" {
		md["server"] = ov.Server
		md["transferred_to_server"] = ov.Server
	}
	if ov.Note != "" {
		md["action_note"] = ov.Note
	}
	md["host_writes"] = false
	n.Metadata = md
	return n
}

func (s *Store) findLocked(id string) (nodes.Node, bool) {
	for _, n := range s.snap.Nodes {
		if n.Id == id {
			return n, true
		}
	}
	for _, n := range s.extraNodes {
		if n.Id == id {
			return n, true
		}
	}
	return nodes.Node{}, false
}

func (s *Store) ensureAccountLocked(login string) string {
	for _, n := range s.snap.Nodes {
		if n.Type == string(nodes.TypeGitHubAccount) && (n.Name == login || n.Id == login) {
			return n.Id
		}
	}
	for _, n := range s.extraNodes {
		if n.Type == string(nodes.TypeGitHubAccount) && (n.Name == login || n.Id == login) {
			return n.Id
		}
	}
	id := spaceID("github_account", login)
	s.extraNodes = append(s.extraNodes, newNode(id, string(nodes.TypeGitHubAccount), login, "", "unknown", s.snap.Source, s.snap.Host, map[string]any{
		"github_account": login,
		"cleanable":      false,
		"unused":         false,
	}))
	return id
}

func confirmWord(action string) (string, bool) {
	switch action {
	case "delete":
		return "DELETE", true
	case "archive":
		return "ARCHIVE", true
	case "transfer":
		return "TRANSFER", true
	default:
		return "", false
	}
}

func protected(n nodes.Node, action string) string {
	if action == "transfer" {
		return ""
	}
	switch n.Type {
	case string(nodes.TypeDisk):
		return "disks are not deleted or archived"
	case string(nodes.TypeGitHubAccount):
		return "GitHub accounts are not deleted or archived from the map"
	}
	if isCoreMeta(n.Metadata) {
		return "core spaces are never deleted or archived"
	}
	return ""
}

func rejectTarget(v string) string {
	if v == "" {
		return ""
	}
	if len(v) > 128 || strings.ContainsAny(v, "\r\n\t") {
		return "transfer target is not a plain name"
	}
	if redact.String(v) != v {
		return "transfer target looks like a secret and was rejected"
	}
	return ""
}

var allowedTypes = map[string]bool{
	string(nodes.TypeDisk):          true,
	string(nodes.TypeVolume):        true,
	string(nodes.TypeContainer):     true,
	string(nodes.TypeDatabase):      true,
	string(nodes.TypeRepo):          true,
	string(nodes.TypeGitHubAccount): true,
}

func sanitize(snap Snapshot) Snapshot {
	snap.HostWrites = false
	snap.Host = redact.String(snap.Host)
	snap.Detail = redact.String(snap.Detail)
	var kept []nodes.Node
	seen := map[string]bool{}
	for _, n := range snap.Nodes {
		if !allowedTypes[n.Type] || n.Id == "" || seen[n.Id] {
			continue
		}
		seen[n.Id] = true
		n.Name = redact.String(n.Name)
		if n.Metadata == nil {
			n.Metadata = map[string]any{}
		}
		n.Metadata = redact.Map(n.Metadata)
		n.Metadata["host_writes"] = false
		if _, ok := n.Metadata["collector"]; !ok && snap.Source != "" {
			n.Metadata["collector"] = snap.Source
		}
		kept = append(kept, n)
	}
	var keptE []edges.Edge
	eseen := map[string]bool{}
	for _, e := range snap.Edges {
		if e.Id == "" || eseen[e.Id] || !seen[e.Source] || !seen[e.Target] {
			continue
		}
		eseen[e.Id] = true
		e.Label = redact.String(e.Label)
		keptE = append(keptE, e)
	}
	if kept == nil {
		kept = []nodes.Node{}
	}
	if keptE == nil {
		keptE = []edges.Edge{}
	}
	snap.Nodes = kept
	snap.Edges = keptE
	return snap
}
