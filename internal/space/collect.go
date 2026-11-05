package space

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Collect reads disks (df) and, when a Docker API is reachable, containers and
// volumes. du is used only for specific volume mountpoints, with a short
// timeout. No command writes to the host.
func Collect(ctx context.Context, logger *zap.SugaredLogger) (Snapshot, error) {
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
	vols, _, probeReasons := probeSizes(ctx, vols, DuBudget(os.Getenv("GOXIO_DU_BUDGET")), 0, func(c context.Context, path string) (int64, error) {
		return duBytes(c, root, path)
	})
	reasons = append(reasons, probeReasons...)

	snap := buildSnapshot(host, time.Now(), disks, vols, ctrs)
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

func duBytes(ctx context.Context, hostRoot, hostPath string) (int64, error) {
	target := hostPath
	if hostRoot != "" && hostRoot != "/" && hostRoot != "." {
		if hostPath == "/" {
			target = hostRoot
		} else {
			target = filepath.Join(hostRoot, strings.TrimPrefix(hostPath, "/"))
		}
	}
	out, err := runCmd(ctx, 2*time.Second, "du", "-sx", "-B1", target)
	if err != nil {
		return 0, err
	}
	sz, ok := parseDu(string(out))
	if !ok {
		return 0, errString("du unreadable")
	}
	return sz, nil
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
