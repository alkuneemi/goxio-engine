package space

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// budgetTree is a stable directory whose top-level du never finishes.
// Each child is one file of 10 bytes. A pass may only spend `budget` steps.
type budgetTree struct{}

func (budgetTree) Du(_ context.Context, path string) (int64, error) {
	if filepath.Clean(path) == "/data" {
		return 0, context.DeadlineExceeded
	}
	return 10, nil
}

func (budgetTree) List(_ context.Context, path string) ([]sizeEntry, error) {
	if filepath.Clean(path) != "/data" {
		return []sizeEntry{{Name: "f", Kind: "file", Bytes: 10}}, nil
	}
	var ents []sizeEntry
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		ents = append(ents, sizeEntry{Name: name, Kind: "dir"})
	}
	return ents, nil
}

func TestSubtreeSizingMakesProgressThenCompletes(t *testing.T) {
	vols := []volumeObs{{Mountpoint: "/data", Kind: "folder", SizeState: "pending"}}
	var work map[string]SizeNode
	var got volumeObs
	for pass := 0; pass < 8; pass++ {
		vols, work, _ = advanceSizes(context.Background(), vols, work, 2, budgetTree{})
		got = vols[0]
		if pass == 0 {
			if got.SizeState == "measured" {
				t.Fatal("a tree larger than the pass budget was marked measured")
			}
			if got.SizeSoFar <= 0 || got.SizeSoFar >= 60 {
				t.Fatalf("first pass made no partial progress: %+v", got)
			}
			if work["/data"].State == "measured" {
				t.Fatal("saved work claimed the directory was finished")
			}
		}
	}
	if got.SizeState != "measured" || got.SizeBytes != 60 {
		t.Fatalf("stable tree did not finish: state=%s bytes=%d sofar=%d progress=%s", got.SizeState, got.SizeBytes, got.SizeSoFar, got.SizeProgress)
	}
}

type unreadWalker struct{}

func (unreadWalker) Du(context.Context, string) (int64, error) { return 0, ErrSizeUnreadable }
func (unreadWalker) List(context.Context, string) ([]sizeEntry, error) {
	return nil, ErrSizeUnreadable
}

func TestUnreadableSizeIsNotZero(t *testing.T) {
	vols := []volumeObs{{Mountpoint: "/secret", Kind: "folder", SizeState: "pending"}}
	vols, _, _ = advanceSizes(context.Background(), vols, nil, 4, unreadWalker{})
	if vols[0].SizeState != "unreadable" {
		t.Fatalf("state = %s", vols[0].SizeState)
	}
	snap := buildSnapshot("synthetic", time.Unix(0, 0), []diskObs{{
		Filesystem: "/dev/sda1", FSType: "ext4", Mount: "/", Size: 100, Used: 1, Avail: 99,
	}}, vols, nil)
	var node nodes.Node
	for _, n := range snap.Nodes {
		if n.Metadata["path"] == "/secret" {
			node = n
		}
	}
	if node.Id == "" {
		t.Fatal("unreadable path was dropped")
	}
	if node.Metadata["size_state"] != "unreadable" {
		t.Fatalf("size_state = %v", node.Metadata["size_state"])
	}
	if _, ok := node.Metadata["size_bytes"]; ok {
		t.Fatalf("unreadable path stored a size: %v", node.Metadata["size_bytes"])
	}
}

func TestMountBoundaryIsNotAddedTwice(t *testing.T) {
	w := mountSplit{}
	vols := []volumeObs{{Mountpoint: "/var", Kind: "folder", SizeState: "pending"}}
	var work map[string]SizeNode
	for i := 0; i < 4; i++ {
		vols, work, _ = advanceSizes(context.Background(), vols, work, 4, w)
	}
	if vols[0].SizeState != "measured" || vols[0].SizeBytes != 5 {
		t.Fatalf("got state=%s bytes=%d (mount bytes must not be included)", vols[0].SizeState, vols[0].SizeBytes)
	}
}

type mountSplit struct{}

func (mountSplit) Du(_ context.Context, path string) (int64, error) {
	if filepath.Clean(path) == "/var" {
		return 0, context.DeadlineExceeded
	}
	return 5, nil
}

func (mountSplit) List(_ context.Context, path string) ([]sizeEntry, error) {
	if filepath.Clean(path) == "/var" {
		return []sizeEntry{
			{Name: "log", Kind: "dir"},
			{Name: "lib", Kind: "mount"},
		}, nil
	}
	return []sizeEntry{{Name: "f", Kind: "file", Bytes: 5}}, nil
}
