package space

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// Synthetic stand-in for a large host inventory. Counts match the shape of a
// real read-only collect (many containers, sized volumes, images, a handful of
// explicit repo labels, mount entries that share a device, and du timeouts).
// Customer names and paths are not copied here.
func TestSyntheticInventoryCompleteness(t *testing.T) {
	const (
		containers = 146
		volumes    = 201
		images     = 64
		databases  = 22
		repos      = 11
	)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	disks := []diskObs{
		{Filesystem: "/dev/sda1", FSType: "ext4", Mount: "/", Size: 1000, Used: 800, Avail: 200},
		{Filesystem: "/dev/sda16", FSType: "ext4", Mount: "/boot", Size: 50, Used: 8, Avail: 42},
		{Filesystem: "/dev/sda15", FSType: "vfat", Mount: "/boot/efi", Size: 20, Used: 1, Avail: 19},
		{Filesystem: "/dev/mapper/appdata", FSType: "ext4", Mount: "/data/app/addons", Size: 80, Used: 2, Avail: 78},
	}
	vols := make([]volumeObs, 0, volumes+images+6)
	for i := 0; i < volumes; i++ {
		vols = append(vols, volumeObs{
			Name:       fmt.Sprintf("vol-%03d", i),
			Mountpoint: fmt.Sprintf("/var/lib/docker/volumes/vol-%03d/_data", i),
			Kind:       "volume",
			SizeBytes:  int64(1000 + i),
			SizeState:  "measured",
			SizeReason: "du",
		})
	}
	for i := 0; i < images; i++ {
		vols = append(vols, volumeObs{
			Name:       fmt.Sprintf("img-%03d:1", i),
			Kind:       "image",
			SizeBytes:  int64(2000 + i),
			SizeState:  "measured",
			SizeReason: "docker",
		})
	}
	timeouts := []string{"/var", "/var/lib", "/var/lib/containerd", "/opt", "/root", "/usr"}
	for _, p := range timeouts {
		vols = append(vols, volumeObs{Mountpoint: p, Kind: pathKind(p), SizeState: "timeout", SizeReason: "timeout"})
	}

	ctrs := make([]containerObs, 0, containers)
	for i := 0; i < containers; i++ {
		c := containerObs{
			ID:           fmt.Sprintf("id-%03d", i),
			Name:         fmt.Sprintf("ctr-%03d", i),
			Image:        "app:1",
			Status:       "running",
			InspectState: "full",
			Mounts: []mountObs{{
				Kind: "volume", Name: fmt.Sprintf("vol-%03d", i%volumes),
				Source: fmt.Sprintf("/var/lib/docker/volumes/vol-%03d/_data", i%volumes),
			}},
		}
		if i >= 60 {
			c.InspectState = "partial"
		}
		if i < databases {
			c.Image = "postgres:16"
			c.DBName = fmt.Sprintf("db-%02d", i)
		}
		if i < repos {
			c.Labels = map[string]string{
				"org.opencontainers.image.source": fmt.Sprintf("https://github.com/octocat/app-%02d.git", i),
			}
		}
		ctrs = append(ctrs, c)
	}

	snap := buildSnapshot("synthetic", now, disks, vols, ctrs)

	var nCtr, nDB, nRepo, nAcct, nImg, nMount int
	groups := map[string]string{}
	mountPaths := map[string]bool{}
	for _, n := range snap.Nodes {
		switch n.Type {
		case string(nodes.TypeContainer):
			nCtr++
		case string(nodes.TypeDatabase):
			nDB++
		case string(nodes.TypeRepo):
			nRepo++
		case string(nodes.TypeGitHubAccount):
			nAcct++
			if n.Name != "octocat" {
				t.Fatalf("unexpected account %q", n.Name)
			}
		case string(nodes.TypeVolume):
			if n.Metadata["kind"] == "image" {
				nImg++
			}
		case string(nodes.TypeDisk):
			if n.Metadata["kind"] == "mount" {
				nMount++
				mountPaths[n.Metadata["path"].(string)] = true
				groups[n.Metadata["device_group"].(string)] = n.Parent
			}
		}
	}
	if nCtr != containers {
		t.Fatalf("containers = %d, want %d (truncated %d)", nCtr, containers, snap.TruncatedCount)
	}
	if snap.TruncatedCount != containers-60 {
		t.Fatalf("truncated = %d, want %d", snap.TruncatedCount, containers-60)
	}
	if snap.Complete {
		t.Fatal("snapshot must not claim complete when inspect is partial or sizes timed out")
	}
	if nDB != databases {
		t.Fatalf("databases = %d, want %d (env names only)", nDB, databases)
	}
	if nRepo != repos {
		t.Fatalf("repos = %d, want %d", nRepo, repos)
	}
	if nAcct != 1 {
		t.Fatalf("accounts = %d, want 1 explicit owner", nAcct)
	}
	if nImg != images {
		t.Fatalf("images = %d, want %d", nImg, images)
	}
	if nMount != 4 {
		t.Fatalf("mount disks = %d, want 4", nMount)
	}
	for _, p := range []string{"/", "/boot", "/boot/efi", "/data/app/addons"} {
		if !mountPaths[p] {
			t.Fatalf("missing mount %s", p)
		}
	}
	if groups["sda"] == "" || groups["mapper/appdata"] == "" {
		t.Fatalf("device groups = %#v", groups)
	}
	if groups["sda"] == groups["mapper/appdata"] {
		t.Fatal("mapper device was collapsed onto sda")
	}
	// Three partitions share one group parent.
	sdaParent := groups["sda"]
	sdaMounts := 0
	for g, parent := range groups {
		if strings.HasPrefix(g, "sda") || g == "sda" {
			if parent == sdaParent {
				sdaMounts++
			}
		}
	}
	if sdaMounts < 1 {
		t.Fatal("sda mounts were not grouped")
	}
	byParent := map[string]int{}
	for _, n := range snap.Nodes {
		if n.Type == string(nodes.TypeDisk) && n.Metadata["kind"] == "mount" && n.Metadata["device_group"] == "sda" {
			byParent[n.Parent]++
		}
	}
	if byParent[sdaParent] != 3 {
		t.Fatalf("sda mount children = %d, want 3 (%v)", byParent[sdaParent], byParent)
	}

	for _, p := range timeouts {
		found := false
		for _, n := range snap.Nodes {
			if n.Metadata["path"] == p {
				found = true
				if n.Metadata["size_state"] != "timeout" {
					t.Fatalf("%s size_state = %v", p, n.Metadata["size_state"])
				}
			}
		}
		if !found {
			t.Fatalf("timed-out path %s was dropped", p)
		}
	}
	// Unlabeled container still present and has no invented account.
	var plain nodes.Node
	for _, n := range snap.Nodes {
		if n.Name == "ctr-100" {
			plain = n
		}
	}
	if plain.Id == "" {
		t.Fatal("unlabeled container missing")
	}
	if plain.Metadata["provenance"] != "unknown" || plain.Metadata["github_account"] != nil {
		t.Fatalf("unlabeled provenance = %#v", plain.Metadata)
	}
	if snap.MissingSizeCount < len(timeouts) {
		t.Fatalf("missing sizes = %d, want at least %d", snap.MissingSizeCount, len(timeouts))
	}
}

func TestProbeSizesResumesAndRecordsTimeouts(t *testing.T) {
	paths := []string{"/var", "/var/lib", "/var/lib/containerd", "/opt", "/root", "/usr", "/var/log", "/tmp"}
	vols := make([]volumeObs, len(paths))
	for i, p := range paths {
		vols[i] = volumeObs{Mountpoint: p, Kind: "folder", SizeState: "pending"}
	}
	timeouts := map[string]bool{
		"/var": true, "/var/lib": true, "/var/lib/containerd": true,
		"/opt": true, "/root": true, "/usr": true,
	}
	run := func(_ context.Context, path string) (int64, error) {
		if timeouts[path] {
			return 0, context.DeadlineExceeded
		}
		return 10, nil
	}
	first, cursor, reasons := probeSizes(context.Background(), vols, 3, 0, run)
	if cursor == 0 {
		t.Fatal("cursor did not advance")
	}
	tried := 0
	for _, v := range first {
		if v.SizeState == "timeout" || v.SizeState == "measured" {
			tried++
		}
	}
	if tried != 3 {
		t.Fatalf("first pass measured %d, want 3", tried)
	}
	if len(reasons) == 0 {
		t.Fatal("expected an explicit partial reason")
	}
	second, _, _ := probeSizes(context.Background(), first, 10, cursor, run)
	seen := map[string]string{}
	for _, v := range second {
		seen[v.Mountpoint] = v.SizeState
	}
	for _, p := range paths {
		st := seen[p]
		if timeouts[p] && st != "timeout" {
			t.Fatalf("%s state = %s, want timeout", p, st)
		}
		if !timeouts[p] && st != "measured" {
			t.Fatalf("%s state = %s, want measured", p, st)
		}
	}
}

func TestEnrichContainersKeepsEveryListing(t *testing.T) {
	const n = 146
	listed := make([]containerObs, n)
	for i := range listed {
		listed[i] = containerObs{ID: fmt.Sprintf("c%03d", i), Name: fmt.Sprintf("n%03d", i)}
	}
	calls := 0
	out, reasons := enrichContainers(context.Background(), listed, func(_ context.Context, c containerObs) (containerObs, error) {
		calls++
		c.InspectState = "full"
		return c, nil
	})
	if len(out) != n || calls != n {
		t.Fatalf("len=%d calls=%d", len(out), calls)
	}
	if len(reasons) != 0 {
		t.Fatalf("reasons = %v", reasons)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls = 0
	out, reasons = enrichContainers(ctx, listed, func(_ context.Context, c containerObs) (containerObs, error) {
		calls++
		if calls == 61 {
			cancel()
			return c, errors.New("stopped")
		}
		c.InspectState = "full"
		return c, nil
	})
	if len(out) != n {
		t.Fatalf("partial len = %d", len(out))
	}
	partial := 0
	for _, c := range out {
		if c.InspectState == "partial" || c.InspectState == "unreadable" {
			partial++
		}
		if c.Name == "" {
			t.Fatal("container dropped its name")
		}
	}
	if partial == 0 || len(reasons) == 0 {
		t.Fatalf("partial=%d reasons=%v", partial, reasons)
	}
}

func TestHostRootRewriteAndSecretPath(t *testing.T) {
	if got := RewriteHostRoot("/host-disk", "/host-disk"); got != "/" {
		t.Fatalf("root rewrite = %s", got)
	}
	if got := RewriteHostRoot("/host-disk/var/lib/docker", "/host-disk"); got != "/var/lib/docker" {
		t.Fatalf("child rewrite = %s", got)
	}
	if got := RewriteHostRoot("/var/log", "/host-disk"); got != "/var/log" {
		t.Fatalf("unrelated rewrite = %s", got)
	}
	red, ok := sensitivePath("/home/app/.ssh/id_ed25519")
	if !ok || strings.Contains(red, "id_ed25519") || !strings.Contains(red, "(redacted)") {
		t.Fatalf("secret path = %s ok=%v", red, ok)
	}
}

func TestDeviceGroupAliases(t *testing.T) {
	if deviceGroup("/dev/sda1") != "sda" || deviceGroup("/dev/sda15") != "sda" || deviceGroup("/dev/sda16") != "sda" {
		t.Fatal("sda partitions did not alias")
	}
	if deviceGroup("/dev/nvme0n1p2") != "nvme0n1" {
		t.Fatalf("nvme = %s", deviceGroup("/dev/nvme0n1p2"))
	}
	if deviceGroup("/dev/mapper/appdata") != "mapper/appdata" {
		t.Fatalf("mapper = %s", deviceGroup("/dev/mapper/appdata"))
	}
	if deviceGroup("/dev/sda1") == deviceGroup("/dev/mapper/appdata") {
		t.Fatal("mapper collapsed onto sda")
	}
}
