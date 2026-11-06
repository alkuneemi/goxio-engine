package space

import (
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// Demo is a fixed server map used when the host collector has nothing to
// show, or when GOXIO_SPACE_SOURCE=demo. It is data, not a running host.
func Demo(now time.Time) Snapshot {
	if now.IsZero() {
		now = time.Unix(0, 0).UTC()
	}
	const (
		host = "demo-host"
		col  = sourceDemo
	)
	n := func(id, typ, name, parent, health string, md map[string]any) nodes.Node {
		return newNode(id, typ, name, parent, health, col, host, md)
	}
	e := func(src, dst, typ, label string) edges.Edge {
		return newEdge(src, dst, typ, label)
	}

	diskA := "space-disk-vda1"
	diskB := "space-disk-vdb1"
	volPG := "space-volume-pgdata"
	volUp := "space-volume-uploads"
	volOld := "space-volume-old-cache"
	volRedis := "space-volume-redis-stale"
	volLog := "space-volume-var-log"
	ctrPG := "space-container-omxio-postgres"
	ctrAPI := "space-container-omxio-api"
	ctrWork := "space-container-omxio-worker"
	ctrRedis := "space-container-abandoned-redis"
	dbOmxio := "space-database-omxio"
	dbTpl := "space-database-template1"
	dbRedis := "space-database-abandoned"
	repoPlat := "space-repo-alkuneemi-omxio-platform"
	repoXIO := "space-repo-alkuneemi-goxio-engine"
	repoOld := "space-repo-old-contractor-forgotten-scripts"
	acctA := "space-github-account-alkuneemi"
	acctOld := "space-github-account-old-contractor"

	nodesOut := []nodes.Node{
		n(diskA, string(nodes.TypeDisk), "/dev/vda1 /", "", "degraded", map[string]any{
			"path": "/dev/vda1", "filesystem": "/dev/vda1", "fs_type": "ext4",
			"size_bytes": int64(80 << 30), "size": "80.0 GB", "used_bytes": int64(61 << 30),
			"disk_pressure": 61.0 / 80.0, "cleanable": false, "unused": false,
		}),
		n(diskB, string(nodes.TypeDisk), "/dev/vdb1 /var/lib/docker", "", "healthy", map[string]any{
			"path": "/var/lib/docker", "filesystem": "/dev/vdb1", "fs_type": "xfs",
			"size_bytes": int64(200 << 30), "size": "200.0 GB", "used_bytes": int64(40 << 30),
			"disk_pressure": 0.20, "cleanable": false, "unused": false,
		}),
		n(volPG, string(nodes.TypeVolume), "pgdata", diskB, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/pgdata/_data", "kind": "volume",
			"size_bytes": int64(12 << 30), "size": "12.0 GB",
			"activity": ActivityCore, "cleanable": false, "unused": false, "core": true,
			"containers": []string{"omxio-postgres"},
		}),
		n(volUp, string(nodes.TypeVolume), "uploads", diskB, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/uploads/_data", "kind": "volume",
			"size_bytes": int64(4 << 30), "size": "4.0 GB",
			"activity": ActivityDaily, "cleanable": false, "unused": false,
			"containers": []string{"omxio-api", "omxio-worker"},
		}),
		n(volOld, string(nodes.TypeVolume), "old-cache", diskA, "unknown", map[string]any{
			"path": "/var/lib/docker/volumes/old-cache/_data", "kind": "volume",
			"size_bytes": int64(18 << 30), "size": "18.0 GB",
			"activity": ActivityIdle, "cleanable": true, "unused": true,
			"containers": []string{},
		}),
		n(volRedis, string(nodes.TypeVolume), "redis-stale", diskB, "unknown", map[string]any{
			"path": "/var/lib/docker/volumes/redis-stale/_data", "kind": "volume",
			"size_bytes": int64(2 << 30), "size": "2.0 GB",
			"activity": ActivityIdle, "cleanable": true, "unused": false,
			"containers": []string{"abandoned-redis"},
		}),
		n(volLog, string(nodes.TypeVolume), "/var/log", diskA, "healthy", map[string]any{
			"path": "/var/log", "kind": "folder",
			"size_bytes": int64(900 << 20), "size": "900.0 MB",
			"activity": ActivityIdle, "cleanable": true, "unused": false,
			"containers": []string{},
		}),
		n(ctrPG, string(nodes.TypeContainer), "omxio-postgres", volPG, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/pgdata/_data", "image": "postgres:16",
			"engine": "postgres", "activity": ActivityCore, "cleanable": false, "unused": false, "core": true,
			"containers": []string{"omxio-postgres"},
			"repo":       repoPlat, "repo_name": "omxio-platform", "github_account": "alkuneemi",
			"size_bytes": int64(12 << 30), "size": "12.0 GB",
		}),
		n(ctrAPI, string(nodes.TypeContainer), "omxio-api", volUp, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/uploads/_data", "image": "omxio-api:local",
			"activity": ActivityDaily, "cleanable": false, "unused": false, "core": false,
			"containers": []string{"omxio-api"},
			"repo":       repoPlat, "repo_name": "omxio-platform", "github_account": "alkuneemi",
			"size_bytes": int64(4 << 30), "size": "4.0 GB",
			"cpu_pressure": 0.22, "ram_pressure": 0.41, "io_pressure": 0.08,
		}),
		n(ctrWork, string(nodes.TypeContainer), "omxio-worker", volUp, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/uploads/_data", "image": "omxio-worker:local",
			"activity": ActivityIntermittent, "cleanable": false, "unused": false,
			"containers": []string{"omxio-worker"},
			"repo":       repoXIO, "repo_name": "goxio-engine", "github_account": "alkuneemi",
			"size_bytes": int64(400 << 20), "size": "400.0 MB",
		}),
		n(ctrRedis, string(nodes.TypeContainer), "abandoned-redis", volRedis, "unknown", map[string]any{
			"path": "/var/lib/docker/volumes/redis-stale/_data", "image": "old-cache:1",
			"activity": ActivityIdle, "cleanable": true, "unused": true, "core": false,
			"containers": []string{"abandoned-redis"},
			"size_bytes": int64(2 << 30), "size": "2.0 GB",
		}),
		n(dbOmxio, string(nodes.TypeDatabase), "omxio", ctrPG, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/pgdata/_data", "engine": "postgres",
			"activity": ActivityCore, "cleanable": false, "unused": false, "core": true,
			"containers": []string{"omxio-postgres"},
			"repo":       repoPlat, "repo_name": "omxio-platform", "github_account": "alkuneemi",
			"size_bytes": int64(8 << 30), "size": "8.0 GB",
		}),
		n(dbTpl, string(nodes.TypeDatabase), "template1", ctrPG, "healthy", map[string]any{
			"path": "/var/lib/docker/volumes/pgdata/_data", "engine": "postgres",
			"activity": ActivityCore, "cleanable": false, "unused": false, "core": true,
			"containers": []string{"omxio-postgres"},
			"repo":       repoPlat, "repo_name": "omxio-platform", "github_account": "alkuneemi",
			"size_bytes": int64(8 << 20), "size": "8.0 MB",
		}),
		n(dbRedis, string(nodes.TypeDatabase), "abandoned", ctrRedis, "unknown", map[string]any{
			"path": "/var/lib/docker/volumes/redis-stale/_data", "engine": "redis",
			"activity": ActivityIdle, "cleanable": true, "unused": true,
			"containers": []string{"abandoned-redis"},
			"size_bytes": int64(500 << 20), "size": "500.0 MB",
		}),
		n(repoPlat, string(nodes.TypeRepo), "omxio-platform", "", "healthy", map[string]any{
			"activity": ActivityCore, "cleanable": false, "unused": false,
			"repo": repoPlat, "repo_name": "omxio-platform", "github_account": "alkuneemi",
			"containers": []string{"omxio-postgres", "omxio-api"},
		}),
		n(repoXIO, string(nodes.TypeRepo), "goxio-engine", "", "healthy", map[string]any{
			"activity": ActivityIntermittent, "cleanable": false, "unused": false,
			"repo": repoXIO, "repo_name": "goxio-engine", "github_account": "alkuneemi",
			"containers": []string{"omxio-worker"},
		}),
		n(repoOld, string(nodes.TypeRepo), "forgotten-scripts", "", "unknown", map[string]any{
			"activity": ActivityIdle, "cleanable": true, "unused": true,
			"repo": repoOld, "repo_name": "forgotten-scripts", "github_account": "old-contractor",
			"containers": []string{},
		}),
		n(acctA, string(nodes.TypeGitHubAccount), "alkuneemi", "", "healthy", map[string]any{
			"cleanable": false, "unused": false, "github_account": "alkuneemi",
		}),
		n(acctOld, string(nodes.TypeGitHubAccount), "old-contractor", "", "healthy", map[string]any{
			"cleanable": false, "unused": false, "github_account": "old-contractor",
		}),
	}

	// Disk path shown on the card is the mount, not the device. The vda node
	// name keeps the device so the card is recognizable; path is the mount.
	nodesOut[0].Metadata["path"] = "/"

	edgesOut := []edges.Edge{
		e(diskA, volOld, edges.TypeContains, "CONTAINS"),
		e(diskA, volLog, edges.TypeContains, "CONTAINS"),
		e(diskB, volPG, edges.TypeContains, "CONTAINS"),
		e(diskB, volUp, edges.TypeContains, "CONTAINS"),
		e(diskB, volRedis, edges.TypeContains, "CONTAINS"),
		e(volPG, ctrPG, edges.TypeContains, "CONTAINS"),
		e(volUp, ctrAPI, edges.TypeContains, "CONTAINS"),
		e(volUp, ctrWork, edges.TypeContains, "CONTAINS"),
		e(volRedis, ctrRedis, edges.TypeContains, "CONTAINS"),
		e(ctrPG, dbOmxio, edges.TypeContains, "CONTAINS"),
		e(ctrPG, dbTpl, edges.TypeContains, "CONTAINS"),
		e(ctrRedis, dbRedis, edges.TypeContains, "CONTAINS"),
		e(acctA, repoPlat, edges.TypeOwns, "OWNS"),
		e(acctA, repoXIO, edges.TypeOwns, "OWNS"),
		e(acctOld, repoOld, edges.TypeOwns, "OWNS"),
		e(repoPlat, ctrPG, edges.TypeDeploysFrom, "DEPLOYS_FROM"),
		e(repoPlat, ctrAPI, edges.TypeDeploysFrom, "DEPLOYS_FROM"),
		e(repoXIO, ctrWork, edges.TypeDeploysFrom, "DEPLOYS_FROM"),
	}

	return Snapshot{
		CollectedAt: now.UTC(),
		Host:        host,
		Source:      sourceDemo,
		HostWrites:  false,
		Detail:      "demo snapshot — host was not modified",
		Nodes:       nodesOut,
		Edges:       edgesOut,
	}
}
