package space

import (
	"strings"
	"testing"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"go.uber.org/zap"
)

func TestParseDFSkipsVirtualFilesystems(t *testing.T) {
	const raw = `
Filesystem     Type     1B-blocks         Used    Available Capacity Mounted on
udev           devtmpfs    1000000            0      1000000       0% /dev
tmpfs          tmpfs       2000000       100000      1900000       5% /run
/dev/vda1      ext4    80000000000  61000000000   19000000000      77% /
/dev/vdb1      xfs    200000000000  40000000000  160000000000      20% /var/lib/docker
overlay        overlay 80000000000  61000000000   19000000000      77% /var/lib/docker/overlay2/abc
`
	disks := parseDF(raw)
	if len(disks) != 2 {
		t.Fatalf("disks = %d, want 2 (%v)", len(disks), disks)
	}
	if disks[0].Mount != "/" || disks[0].FSType != "ext4" || disks[0].Used != 61000000000 {
		t.Fatalf("first disk = %+v", disks[0])
	}
	if disks[1].Mount != "/var/lib/docker" {
		t.Fatalf("second mount = %s", disks[1].Mount)
	}
}

func TestBuildSnapshotHierarchy(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	snap := buildSnapshot("box", now, []diskObs{
		{Filesystem: "/dev/vdb1", FSType: "xfs", Mount: "/var/lib/docker", Size: 100, Used: 10, Avail: 90},
		{Filesystem: "/dev/vda1", FSType: "ext4", Mount: "/", Size: 100, Used: 80, Avail: 20},
	}, []volumeObs{
		{Name: "pgdata", Mountpoint: "/var/lib/docker/volumes/pgdata/_data", SizeBytes: 50},
		{Name: "orphan", Mountpoint: "/var/lib/docker/volumes/orphan/_data", SizeBytes: 10},
	}, []containerObs{
		{
			ID: "abc", Name: "omxio-postgres", Image: "postgres:16", Status: "running",
			StartedAt: now.Add(-2 * time.Hour),
			Labels: map[string]string{
				"org.opencontainers.image.source": "https://github.com/alkuneemi/omxio-platform.git",
			},
			DBName: "omxio",
			Mounts: []mountObs{{Kind: "volume", Name: "pgdata", Source: "/var/lib/docker/volumes/pgdata/_data", Dest: "/var/lib/postgresql/data"}},
		},
	})

	byID := indexNodes(snap.Nodes)
	dbID := spaceID("database", "omxio-postgres-omxio")
	db, ok := byID[dbID]
	if !ok {
		t.Fatalf("missing database %s in %d nodes", dbID, len(snap.Nodes))
	}
	if db.Parent == "" || byID[db.Parent].Type != string(nodes.TypeContainer) {
		t.Fatalf("database parent = %s", db.Parent)
	}
	ctr := byID[db.Parent]
	if ctr.Parent == "" || byID[ctr.Parent].Type != string(nodes.TypeVolume) {
		t.Fatalf("container parent = %s", ctr.Parent)
	}
	vol := byID[ctr.Parent]
	if vol.Parent == "" || byID[vol.Parent].Type != string(nodes.TypeDisk) {
		t.Fatalf("volume parent = %s", vol.Parent)
	}
	if byID[vol.Parent].Name == "" || !strings.Contains(byID[vol.Parent].Metadata["path"].(string), "/var/lib/docker") {
		t.Fatalf("volume should sit on the docker disk, got %+v", byID[vol.Parent])
	}
	repoID, _ := db.Metadata["repo"].(string)
	repo, ok := byID[repoID]
	if !ok || repo.Type != string(nodes.TypeRepo) || repo.Name != "omxio-platform" {
		t.Fatalf("repo = %+v", repo)
	}
	acct := byID[spaceID("github_account", "alkuneemi")]
	if acct.Type != string(nodes.TypeGitHubAccount) {
		t.Fatalf("account missing: %+v", acct)
	}
	if !hasEdge(snap.Edges, acct.Id, repo.Id, edges.TypeOwns) {
		t.Fatal("missing OWNS edge")
	}
	if !hasEdge(snap.Edges, repo.Id, ctr.Id, edges.TypeDeploysFrom) {
		t.Fatal("missing DEPLOYS_FROM edge")
	}
	orphan := byID[spaceID("volume", "orphan")]
	if orphan.Metadata["unused"] != true || orphan.Metadata["cleanable"] != true {
		t.Fatalf("orphan volume = %+v", orphan.Metadata)
	}
	if db.Metadata["activity"] != ActivityCore {
		t.Fatalf("postgres database activity = %v", db.Metadata["activity"])
	}
	if db.Metadata["host_writes"] != false {
		t.Fatal("host_writes must be false")
	}
}

func TestDemoChainReachesGitHubAccount(t *testing.T) {
	snap := Demo(time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	byID := indexNodes(snap.Nodes)
	db := byID["space-database-omxio"]
	if db.Metadata["github_account"] != "alkuneemi" {
		t.Fatalf("database account = %v", db.Metadata["github_account"])
	}
	// disk → volume → container → database via parents
	cur := db
	seen := map[string]bool{}
	var types []string
	for cur.Id != "" && !seen[cur.Id] {
		seen[cur.Id] = true
		types = append(types, cur.Type)
		if cur.Parent == "" {
			break
		}
		cur = byID[cur.Parent]
	}
	got := strings.Join(types, ">")
	if got != "database>container>volume>disk" {
		t.Fatalf("parent chain = %s", got)
	}
	if byID["space-volume-old-cache"].Metadata["unused"] != true {
		t.Fatal("old-cache should be unused")
	}
	if byID["space-container-omxio-postgres"].Metadata["activity"] != ActivityCore {
		t.Fatal("postgres container should be core")
	}
}

func TestActionsRequireConfirmAndNeverTouchHost(t *testing.T) {
	s := NewStore(zap.NewNop().Sugar())
	s.Load(Demo(time.Now()))

	if _, err := s.Apply(ActionRequest{NodeID: "space-volume-old-cache", Action: "delete", Confirm: "delete"}); err == nil {
		t.Fatal("lowercase confirm must fail")
	}
	node := mustNode(t, s, "space-volume-old-cache")
	if node.Metadata["pending_delete"] == true {
		t.Fatal("failed confirm must not mark the node")
	}

	if _, err := s.Apply(ActionRequest{NodeID: "space-container-omxio-postgres", Action: "delete", Confirm: "DELETE"}); err == nil {
		t.Fatal("core delete must fail")
	}
	if _, err := s.Apply(ActionRequest{NodeID: "space-disk-vda1", Action: "archive", Confirm: "ARCHIVE"}); err == nil {
		t.Fatal("disk archive must fail")
	}

	rec, err := s.Apply(ActionRequest{NodeID: "space-volume-old-cache", Action: "delete", Confirm: "DELETE"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.AppliedToHost {
		t.Fatal("applied_to_host must stay false")
	}
	node = mustNode(t, s, "space-volume-old-cache")
	if node.Metadata["pending_delete"] != true || node.Metadata["host_writes"] != false {
		t.Fatalf("overlay = %+v", node.Metadata)
	}

	if _, err := s.Apply(ActionRequest{NodeID: "space-repo-alkuneemi-omxio-platform", Action: "transfer", Confirm: "TRANSFER"}); err == nil {
		t.Fatal("transfer without a target must fail")
	}
	if _, err := s.Apply(ActionRequest{
		NodeID: "space-repo-alkuneemi-omxio-platform", Action: "transfer", Confirm: "TRANSFER",
		TargetAccount: "other-org", TargetServer: "srv-2",
	}); err != nil {
		t.Fatal(err)
	}
	repo := mustNode(t, s, "space-repo-alkuneemi-omxio-platform")
	if repo.Metadata["github_account"] != "other-org" || repo.Metadata["server"] != "srv-2" {
		t.Fatalf("transfer metadata = %+v", repo.Metadata)
	}
	g := s.Graph()
	if !hasEdge(g.Edges, spaceID("github_account", "other-org"), repo.Id, edges.TypeOwns) {
		t.Fatal("ownership edge was not retargeted")
	}
	for _, e := range g.Edges {
		if e.Type == edges.TypeOwns && e.Target == repo.Id && e.Source == "space-github-account-alkuneemi" {
			t.Fatal("old owner edge still present")
		}
	}
}

func TestIngestMasksSecrets(t *testing.T) {
	s := NewStore(zap.NewNop().Sugar())
	body := `{
	  "host": "box",
	  "nodes": [{
	    "id": "space-volume-leak",
	    "type": "volume",
	    "name": "leak",
	    "health": "unknown",
	    "metadata": {
	      "path": "postgres://omxio:supersecret@db.internal/omxio",
	      "password": "supersecret",
	      "cleanable": true,
	      "unused": true
	    }
	  }],
	  "edges": []
	}`
	if err := s.Ingest(strings.NewReader(body), ""); err != nil {
		t.Fatal(err)
	}
	n := mustNode(t, s, "space-volume-leak")
	path, _ := n.Metadata["path"].(string)
	if strings.Contains(path, "supersecret") {
		t.Fatalf("path leaked secret: %s", path)
	}
	if n.Metadata["password"] != "***" {
		t.Fatalf("password = %v", n.Metadata["password"])
	}
	if n.Metadata["host_writes"] != false {
		t.Fatal("ingest must force host_writes false")
	}
	if s.Source() != "ingest" {
		t.Fatalf("source = %s", s.Source())
	}
}

func TestIngestToken(t *testing.T) {
	s := NewStore(zap.NewNop().Sugar())
	s.SetIngestToken("tok")
	err := s.Ingest(strings.NewReader(`{"nodes":[{"id":"space-disk-a","type":"disk","name":"a","health":"healthy"}]}`), "nope")
	if err == nil {
		t.Fatal("expected token rejection")
	}
}

func TestMergeKeepsDiscoveryNodes(t *testing.T) {
	s := NewStore(zap.NewNop().Sugar())
	s.Load(Demo(time.Now()))
	base := &graph.Graph{
		Nodes: []nodes.Node{
			{Id: "space-disk-vda1", Type: "disk", Name: "from-discovery", Metadata: map[string]any{}},
			{Id: "pg-test", Type: "database", Name: "test", Metadata: map[string]any{}},
		},
		Edges: []edges.Edge{},
	}
	merged := s.Merge(base)
	var disks int
	for _, n := range merged.Nodes {
		if n.Id == "space-disk-vda1" {
			disks++
			if n.Name != "from-discovery" {
				t.Fatalf("discovery node replaced: %s", n.Name)
			}
		}
	}
	if disks != 1 {
		t.Fatalf("disk copies = %d", disks)
	}
	foundPG := false
	foundVolume := false
	for _, n := range merged.Nodes {
		if n.Id == "pg-test" {
			foundPG = true
		}
		if n.Id == "space-volume-old-cache" {
			foundVolume = true
		}
	}
	if !foundPG || !foundVolume {
		t.Fatalf("merge dropped nodes pg=%v volume=%v", foundPG, foundVolume)
	}
}

func indexNodes(ns []nodes.Node) map[string]nodes.Node {
	out := make(map[string]nodes.Node, len(ns))
	for _, n := range ns {
		out[n.Id] = n
	}
	return out
}

func hasEdge(es []edges.Edge, src, dst, typ string) bool {
	for _, e := range es {
		if e.Source == src && e.Target == dst && e.Type == typ {
			return true
		}
	}
	return false
}

func mustNode(t *testing.T, s *Store, id string) nodes.Node {
	t.Helper()
	for _, n := range s.Graph().Nodes {
		if n.Id == id {
			return n
		}
	}
	t.Fatalf("node %s not found", id)
	return nodes.Node{}
}
