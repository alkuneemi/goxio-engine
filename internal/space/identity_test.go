package space

import (
	"testing"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

func TestVolumeIdentityDedupAndImageDigest(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	disks := []diskObs{{Filesystem: "/dev/sda1", FSType: "ext4", Mount: "/", Size: 100, Used: 10, Avail: 90}}
	vols := []volumeObs{
		{Name: "pgdata", Mountpoint: "/var/lib/docker/volumes/pgdata/_data", Kind: "volume", SizeBytes: 50, SizeState: "measured"},
		{Name: "pgdata", Mountpoint: "/var/lib/docker/volumes/pgdata/_data", Kind: "volume", SizeBytes: 50, SizeState: "measured"},
		{Mountpoint: "/var/lib/docker/volumes/pgdata/_data", Kind: "folder", SizeState: "pending"},
		{Name: "app:1", Kind: "image", Digest: "sha256:abc", Tags: []string{"app:1"}, SizeBytes: 9, SizeState: "measured"},
		{Name: "app:2", Kind: "image", Digest: "sha256:abc", Tags: []string{"app:2"}, SizeBytes: 9, SizeState: "measured"},
		{Name: "other:1", Kind: "image", Digest: "sha256:def", Tags: []string{"other:1"}, SizeBytes: 4, SizeState: "measured"},
		{Mountpoint: "/var/log", Kind: "log", SizeState: "timeout", SizeReason: "timeout"},
	}
	snap := buildSnapshot("synthetic", now, disks, vols, nil)
	snap = FillInventory(snap)
	if snap.Volumes != 1 {
		t.Fatalf("volumes = %d, want 1", snap.Volumes)
	}
	if snap.Images != 2 {
		t.Fatalf("images = %d, want 2 distinct digests", snap.Images)
	}
	if snap.Folders != 1 {
		t.Fatalf("folders = %d, want the log path only", snap.Folders)
	}
	if snap.SizesTimeout != 1 || snap.SizesMeasured < 3 {
		t.Fatalf("sizes measured=%d timeout=%d pending=%d", snap.SizesMeasured, snap.SizesTimeout, snap.SizesPending)
	}
	var image nodes.Node
	for _, n := range snap.Nodes {
		if n.Metadata["digest"] == "sha256:abc" {
			image = n
		}
		if n.Metadata["kind"] == "folder" && n.Metadata["path"] == "/var/lib/docker/volumes/pgdata/_data" {
			t.Fatal("bind path duplicated a volume mountpoint")
		}
	}
	if image.Id == "" || image.Metadata["kind"] != "image" {
		t.Fatalf("image node = %#v", image.Metadata)
	}
	tags, _ := image.Metadata["tags"].([]string)
	if len(tags) != 2 {
		t.Fatalf("tags = %#v", image.Metadata["tags"])
	}
}

func TestMergeKeepsMeasuredSize(t *testing.T) {
	prev := Snapshot{Nodes: []nodes.Node{{
		Id: "space-folder-var", Type: "volume", Name: "/var",
		Metadata: map[string]any{"path": "/var", "kind": "folder", "size_state": "measured", "size_bytes": float64(10), "size": "10 B"},
	}}}
	next := Snapshot{Nodes: []nodes.Node{{
		Id: "space-folder-var", Type: "volume", Name: "/var",
		Metadata: map[string]any{"path": "/var", "kind": "folder", "size_state": "timeout"},
	}}}
	got := MergeSizeProgress(prev, next)
	if got.Nodes[0].Metadata["size_state"] != "measured" {
		t.Fatalf("size_state = %v", got.Nodes[0].Metadata["size_state"])
	}
	if _, ok := got.Nodes[0].Metadata["size_bytes"]; !ok {
		t.Fatal("measured size was dropped")
	}
}
