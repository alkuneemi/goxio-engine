package portal

import (
	"strings"
	"testing"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/space"
)

func TestCompanyNameSetAndProfile(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := p.CreateAccount("sahabify", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.AddAlias("sahabify", "sahabify@gmail.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.AddServer(acct.ID, "box-a", "203.0.113.10"); err != nil {
		t.Fatal(err)
	}
	got, err := p.SetCompanyName(acct.ID, "  سحاب الحلول الذكية  ")
	if err != nil {
		t.Fatal(err)
	}
	if got.CompanyName != "سحاب الحلول الذكية" {
		t.Fatalf("company %q", got.CompanyName)
	}
	if got.Login != "sahabify" || got.Email != "sahabify@gmail.com" || got.ID != acct.ID {
		t.Fatalf("profile identity %+v", got)
	}
	if got.ServerCount != 1 {
		t.Fatalf("server_count %d", got.ServerCount)
	}
	if got.CreatedAt == "" {
		t.Fatal("missing created_at")
	}
	again, err := p.Profile(acct.ID)
	if err != nil || again.CompanyName != "سحاب الحلول الذكية" {
		t.Fatalf("reload %+v %v", again, err)
	}
	cleared, err := p.SetCompanyName(acct.ID, "   ")
	if err != nil || cleared.CompanyName != "" {
		t.Fatalf("clear %+v %v", cleared, err)
	}
}

func TestCompanyNameRejectedWhenTooLong(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := p.CreateAccount("sahabify", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SetCompanyName(acct.ID, strings.Repeat("س", 81)); err == nil {
		t.Fatal("expected too-long company name to fail")
	}
}

func TestListServersSummarizesSnapshotAndMarksSharedAddress(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := p.CreateAccount("sahabify", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	older, _, err := p.AddServer(acct.ID, "first", "187.127.114.87")
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := p.AddServer(acct.ID, "second", "187.127.114.87")
	if err != nil {
		t.Fatal(err)
	}
	oldSnap := space.Snapshot{
		CollectedAt:      time.Date(2026, 11, 5, 20, 1, 0, 0, time.UTC),
		ListedContainers: 10,
		Nodes: []nodes.Node{
			{Id: "c1", Type: "container", Name: "c1"},
			{Id: "db1", Type: "database", Name: "db1"},
			{Id: "v1", Type: "volume", Name: "v1"},
		},
	}
	newSnap := space.Snapshot{
		CollectedAt:      time.Date(2026, 11, 5, 20, 33, 0, 0, time.UTC),
		ListedContainers: 146,
		Databases:        22,
		Volumes:          201,
		Images:           64,
		Nodes: []nodes.Node{
			{Id: "root", Type: "disk", Name: "root", Metadata: map[string]any{
				"path": "/", "size_state": "measured",
				"size_bytes": float64(414921494528), "used_bytes": float64(378922766336),
			}},
		},
	}
	if err := p.SaveSnapshot(acct.ID, older.ID, oldSnap); err != nil {
		t.Fatal(err)
	}
	if err := p.SaveSnapshot(acct.ID, newer.ID, newSnap); err != nil {
		t.Fatal(err)
	}
	list, err := p.ListServers(acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("len %d", len(list))
	}
	if list[0].ID != newer.ID {
		t.Fatalf("expected newer snapshot first, got %s then %s", list[0].Name, list[1].Name)
	}
	if !list[0].SharedAddress || !list[1].SharedAddress {
		t.Fatalf("shared_address not marked: %+v %+v", list[0], list[1])
	}
	if list[0].Containers != 146 || list[0].Databases != 22 || list[0].Volumes != 201 || list[0].Images != 64 {
		t.Fatalf("newer counts %+v", list[0])
	}
	if list[0].DiskUsedBytes != 378922766336 || list[0].DiskSizeBytes != 414921494528 {
		t.Fatalf("disk %d/%d", list[0].DiskUsedBytes, list[0].DiskSizeBytes)
	}
	if list[1].Containers != 10 || list[1].Databases != 1 || list[1].Volumes != 1 {
		t.Fatalf("older fallback counts %+v", list[1])
	}
}

func TestServerSummaryKeepsVolumeAndFolderCounts(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := p.CreateAccount("ksa", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	srv, _, err := p.AddServer(acct.ID, "KSA", "13.140.208.109")
	if err != nil {
		t.Fatal(err)
	}
	snap := space.Snapshot{
		CollectedAt:      time.Date(2026, 11, 11, 3, 29, 0, 0, time.UTC),
		ListedContainers: 112,
		Databases:        20,
		Volumes:          106,
		Folders:          423,
		Images:           46,
	}
	if err := p.SaveSnapshot(acct.ID, srv.ID, snap); err != nil {
		t.Fatal(err)
	}
	list, err := p.ListServers(acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len %d", len(list))
	}
	if list[0].Volumes != 106 || list[0].Folders != 423 {
		t.Fatalf("volumes=%d folders=%d", list[0].Volumes, list[0].Folders)
	}
	if list[0].Containers != 112 || list[0].Databases != 20 || list[0].Images != 46 {
		t.Fatalf("other counts %+v", list[0])
	}
}

func TestServerSummaryCountsFolderNodesWhenFieldIsEmpty(t *testing.T) {
	p, err := Open(t.TempDir(), testOpts())
	if err != nil {
		t.Fatal(err)
	}
	acct, err := p.CreateAccount("ksa", "password-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	srv, _, err := p.AddServer(acct.ID, "KSA", "13.140.208.109")
	if err != nil {
		t.Fatal(err)
	}
	snap := space.Snapshot{
		Volumes: 106,
		Nodes: []nodes.Node{
			{Id: "vol", Type: "volume", Name: "vol", Metadata: map[string]any{"kind": "volume"}},
			{Id: "dir", Type: "volume", Name: "dir", Metadata: map[string]any{"kind": "folder"}},
		},
	}
	if err := p.SaveSnapshot(acct.ID, srv.ID, snap); err != nil {
		t.Fatal(err)
	}
	list, err := p.ListServers(acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Volumes != 106 || list[0].Folders != 1 {
		t.Fatalf("volumes=%d folders=%d", list[0].Volumes, list[0].Folders)
	}
}
