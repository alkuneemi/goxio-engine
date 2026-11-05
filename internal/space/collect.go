package space

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

// Collect reads disks (df) and, when a Docker API is reachable, containers and
// volumes. du is bounded per pass and does not write to the host.
func Collect(ctx context.Context, logger *zap.SugaredLogger) (Snapshot, error) {
	return CollectResume(ctx, logger, nil)
}

// CollectResume repeats Collect and keeps sizes that were already measured in
// prev. Paths that timed out are tried again with a longer timeout. The HTTP
// API does not call this; it only stores snapshots a collector posts.
func CollectResume(ctx context.Context, logger *zap.SugaredLogger, prev *Snapshot) (Snapshot, error) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "host"
	}

	var notes []string
	disks, derr := readDisks(ctx)
	if derr != nil {
		notes = append(notes, "df: "+derr.Error())
		logger.Infow("space collector: df failed", "err", derr)
	}

	root := strings.TrimSpace(os.Getenv("GOXIO_HOST_ROOT"))
	for i := range disks {
		disks[i].Mount = RewriteHostRoot(disks[i].Mount, root)
	}
	vols, ctrs, reasons, xerr := readDocker(ctx, logger)
	if xerr != nil {
		notes = append(notes, "docker: "+xerr.Error())
		logger.Infow("space collector: docker unavailable", "err", xerr)
	}
	for i := range vols {
		if vols[i].Mountpoint != "" {
			vols[i].Mountpoint = RewriteHostRoot(vols[i].Mountpoint, root)
		}
	}
	vols = addPathRoots(vols, disks, existingPathRoots(root))
	vols = rememberSizes(vols, prev)
	work := map[string]SizeNode{}
	if prev != nil {
		for k, v := range prev.SizeWork {
			work[k] = v
		}
	}
	vols, work, _ = advanceSizes(ctx, vols, work, DuBudget(os.Getenv("GOXIO_DU_BUDGET")), hostWalker{root: root})

	snap := buildSnapshot(host, time.Now(), disks, vols, ctrs)
	snap.SizeWork = work
	snap.PartialReasons = append(snap.PartialReasons, append(notes, reasons...)...)
	snap.Partial = len(snap.PartialReasons) > 0
	snap.Complete = !snap.Partial
	if len(notes) > 0 {
		snap.Detail = strings.Join(notes, "; ")
	}
	if len(snap.Nodes) == 0 && len(notes) > 0 {
		return snap, errString(snap.Detail)
	}
	return snap, nil
}

func addPathRoots(vols []volumeObs, disks []diskObs, roots []volumeObs) []volumeObs {
	mounts := map[string]bool{}
	for _, d := range disks {
		if d.Mount != "" {
			mounts[filepath.Clean(d.Mount)] = true
		}
	}
	have := map[string]bool{}
	for _, v := range vols {
		if v.Mountpoint != "" {
			have[filepath.Clean(v.Mountpoint)] = true
		}
	}
	for _, r := range roots {
		p := filepath.Clean(r.Mountpoint)
		if p == "" || mounts[p] || have[p] {
			continue
		}
		vols = append(vols, r)
		have[p] = true
	}
	return vols
}

func existingPathRoots(hostRoot string) []volumeObs {
	var out []volumeObs
	for _, p := range PathRoots() {
		target := p
		if hostRoot != "" && hostRoot != "/" && hostRoot != "." {
			target = filepath.Join(hostRoot, strings.TrimPrefix(p, "/"))
		}
		fi, err := os.Stat(target)
		if err != nil || !fi.IsDir() {
			continue
		}
		out = append(out, volumeObs{Mountpoint: p, Kind: pathKind(p), SizeState: "pending"})
	}
	return out
}

func duBytes(ctx context.Context, hostRoot, hostPath string, timeout time.Duration) (int64, error) {
	target := hostPath
	if hostRoot != "" && hostRoot != "/" && hostRoot != "." {
		if hostPath == "/" {
			target = hostRoot
		} else {
			target = filepath.Join(hostRoot, strings.TrimPrefix(hostPath, "/"))
		}
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	out, err := runCmd(ctx, timeout, "du", "-sx", "-B1", target)
	if err != nil {
		return 0, err
	}
	sz, ok := parseDu(string(out))
	if !ok {
		return 0, errString("du unreadable")
	}
	return sz, nil
}

func rememberSizes(vols []volumeObs, prev *Snapshot) []volumeObs {
	if prev == nil {
		return vols
	}
	type mem struct {
		state  string
		reason string
		size   int64
	}
	readSize := func(n nodes.Node) int64 {
		switch v := n.Metadata["size_bytes"].(type) {
		case int64:
			return v
		case float64:
			return int64(v)
		case int:
			return int64(v)
		default:
			return 0
		}
	}
	byPath := map[string]mem{}
	byVol := map[string]mem{}
	byDigest := map[string]mem{}
	for _, n := range prev.Nodes {
		if n.Metadata == nil {
			continue
		}
		st := sizeStateOf(n)
		if st != "measured" && st != "timeout" && st != "unreadable" {
			continue
		}
		m := mem{state: st, reason: st, size: readSize(n)}
		if p, ok := n.Metadata["path"].(string); ok && p != "" {
			byPath[filepath.Clean(p)] = m
		}
		if n.Metadata["kind"] == "volume" {
			if name, ok := n.Metadata["volume_name"].(string); ok && name != "" {
				byVol[name] = m
			}
		}
		if d, ok := n.Metadata["digest"].(string); ok && d != "" {
			byDigest[d] = m
		}
	}
	for i, v := range vols {
		var m mem
		ok := false
		switch {
		case v.Kind == "image" && v.Digest != "":
			m, ok = byDigest[v.Digest]
		case v.Name != "" && (v.Kind == "volume" || v.Kind == ""):
			m, ok = byVol[v.Name]
		}
		if !ok && v.Mountpoint != "" {
			m, ok = byPath[filepath.Clean(v.Mountpoint)]
		}
		if !ok {
			continue
		}
		if m.state == "measured" && m.size > 0 {
			vols[i].SizeBytes = m.size
			vols[i].SizeState = "measured"
			vols[i].SizeReason = "previous"
			continue
		}
		if vols[i].SizeState == "" || vols[i].SizeState == "pending" || vols[i].SizeState == "unknown" {
			vols[i].SizeState = m.state
			vols[i].SizeReason = m.reason
		}
	}
	return vols
}

type errString string

func (e errString) Error() string { return string(e) }

func readDisks(ctx context.Context) ([]diskObs, error) {
	out, err := runCmd(ctx, 4*time.Second, "df", "-B1", "-PT")
	if err != nil {
		return nil, err
	}
	return parseDF(string(out)), nil
}

func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	return cmd.Output()
}
