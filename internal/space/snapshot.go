// Package space is the GoXIO disk → volume → container → database → repo →
// GitHub account map. The collector only reads the host. Actions record an
// intent and never write to the host.
package space

import (
	"fmt"
	"strings"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph"
	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/redact"
)

const (
	ActivityDaily        = "daily"
	ActivityIntermittent = "intermittent"
	ActivityIdle         = "idle"
	ActivityCore         = "core"

	sourceHost   = "host"
	sourceDemo   = "demo"
	sourceIngest = "ingest"
)

// Snapshot is one read-only view of a server's space hierarchy.
type Snapshot struct {
	CollectedAt         time.Time    `json:"collected_at"`
	Host                string       `json:"host"`
	Source              string       `json:"source"`
	HostWrites          bool         `json:"host_writes"`
	Detail              string       `json:"detail,omitempty"`
	Partial             bool         `json:"partial"`
	PartialReasons      []string     `json:"partial_reasons,omitempty"`
	Complete            bool         `json:"complete"`
	MissingSizeCount    int          `json:"missing_size_count"`
	TruncatedCount      int          `json:"truncated_count"`
	ListedContainers    int          `json:"listed_containers"`
	InspectedContainers int          `json:"inspected_containers"`
	Nodes               []nodes.Node `json:"nodes"`
	Edges               []edges.Edge `json:"edges"`
}

// HasSpace reports whether the snapshot found a disk, volume, or container.
func (s Snapshot) HasSpace() bool {
	for _, n := range s.Nodes {
		switch n.Type {
		case string(nodes.TypeDisk), string(nodes.TypeVolume), string(nodes.TypeContainer):
			return true
		}
	}
	return false
}

// Graph returns a copy of the snapshot as a graph. Nil slices become empty
// arrays so JSON clients never see null.
func (s Snapshot) Graph() *graph.Graph {
	ns := append([]nodes.Node{}, s.Nodes...)
	es := append([]edges.Edge{}, s.Edges...)
	if ns == nil {
		ns = []nodes.Node{}
	}
	if es == nil {
		es = []edges.Edge{}
	}
	return &graph.Graph{Nodes: ns, Edges: es}
}

func newNode(id, typ, name, parent, health, collector, server string, md map[string]any) nodes.Node {
	if md == nil {
		md = map[string]any{}
	}
	md["collector"] = collector
	if server != "" {
		md["server"] = server
	}
	md["host_writes"] = false
	if health == "" {
		health = "unknown"
	}
	return nodes.Node{
		Id:       id,
		Type:     typ,
		Name:     name,
		Parent:   parent,
		Health:   health,
		Metadata: redact.Map(md),
	}
}

func newEdge(src, dst, typ, label string) edges.Edge {
	return edges.Edge{
		Id:     "e-" + typ + "-" + src + "-" + dst,
		Source: src,
		Target: dst,
		Type:   typ,
		Label:  label,
	}
}

func putStr(md map[string]any, key, val string) {
	if val != "" {
		md[key] = val
	}
}

func putSize(md map[string]any, size int64) {
	if size > 0 {
		md["size_bytes"] = size
		md["size"] = humanBytes(size)
	}
}

func putFloat(md map[string]any, key string, val float64, ok bool) {
	if ok {
		md[key] = val
	}
}

func humanBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

func spaceID(kind, raw string) string {
	slug := slug(raw)
	if slug == "" {
		slug = "unknown"
	}
	return "space-" + kind + "-" + slug
}

func slug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func hotter(a, b string) string {
	if activityRank(a) >= activityRank(b) {
		return a
	}
	return b
}

func activityRank(a string) int {
	switch a {
	case ActivityIdle:
		return 1
	case ActivityIntermittent:
		return 2
	case ActivityDaily:
		return 3
	case ActivityCore:
		return 4
	default:
		return 0
	}
}

func isCoreMeta(md map[string]any) bool {
	if md == nil {
		return false
	}
	if b, ok := md["core"].(bool); ok && b {
		return true
	}
	if a, _ := md["activity"].(string); a == ActivityCore {
		return true
	}
	return false
}
