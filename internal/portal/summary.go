package portal

import (
	"os"
	"sort"
	"strings"

	"github.com/guilherme-grimm/graph-go/internal/space"
)

func (s *Store) readSnapshot(accountID, serverID string) (space.Snapshot, bool, error) {
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

func (s *Store) serverView(srv Server) ServerView {
	view := srv.view()
	if snap, ok, err := s.readSnapshot(srv.AccountID, srv.ID); err == nil && ok {
		fillServerSummary(&view, snap)
	}
	return view
}

func fillServerSummary(view *ServerView, snap space.Snapshot) {
	if !snap.CollectedAt.IsZero() {
		view.CollectedAt = snap.CollectedAt.UTC()
	}
	view.Containers = snap.ListedContainers
	if view.Containers == 0 {
		view.Containers = countNodes(snap, "container")
	}
	view.Databases = snap.Databases
	if view.Databases == 0 {
		view.Databases = countNodes(snap, "database")
	}
	view.Volumes = snap.Volumes
	if view.Volumes == 0 {
		view.Volumes = countNodes(snap, "volume")
	}
	view.Images = snap.Images
	if view.Images == 0 {
		view.Images = countNodes(snap, "image")
	}
	view.DiskUsedBytes, view.DiskSizeBytes = diskUsage(snap)
}

func countNodes(snap space.Snapshot, typ string) int {
	n := 0
	for _, node := range snap.Nodes {
		if node.Type == typ {
			n++
		}
	}
	return n
}

func diskUsage(snap space.Snapshot) (used, total int64) {
	var bestUsed, bestTotal int64
	for _, node := range snap.Nodes {
		if node.Type != "disk" || node.Metadata == nil {
			continue
		}
		if metaString(node.Metadata["size_state"]) != "measured" {
			continue
		}
		size := metaInt64(node.Metadata["size_bytes"])
		got := metaInt64(node.Metadata["used_bytes"])
		if size <= 0 && got <= 0 {
			continue
		}
		if metaString(node.Metadata["path"]) == "/" {
			return got, size
		}
		if size > bestTotal {
			bestTotal, bestUsed = size, got
		}
	}
	return bestUsed, bestTotal
}

func metaString(v any) string {
	s, _ := v.(string)
	return s
}

func metaInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case float32:
		return int64(n)
	default:
		return 0
	}
}

func markSharedAddresses(list []ServerView) {
	counts := map[string]int{}
	for _, srv := range list {
		addr := strings.TrimSpace(srv.Address)
		if addr == "" {
			continue
		}
		counts[addr]++
	}
	for i := range list {
		addr := strings.TrimSpace(list[i].Address)
		list[i].SharedAddress = addr != "" && counts[addr] > 1
	}
}

func sortServerViews(list []ServerView) {
	sort.SliceStable(list, func(i, j int) bool {
		ti, tj := list[i].CollectedAt, list[j].CollectedAt
		if ti.IsZero() {
			ti = list[i].UpdatedAt
		}
		if tj.IsZero() {
			tj = list[j].UpdatedAt
		}
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return list[i].Name < list[j].Name
	})
}
