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

	vols, ctrs, xerr := readDocker(ctx, logger)
	if xerr != nil {
		notes = append(notes, "docker: "+xerr.Error())
		logger.Infow("space collector: docker unavailable", "err", xerr)
	}
	vols = fillSizes(ctx, vols)

	snap := buildSnapshot(host, time.Now(), disks, vols, ctrs)
	if len(notes) > 0 {
		snap.Detail = strings.Join(notes, "; ")
	}
	if len(snap.Nodes) == 0 && len(notes) > 0 {
		return snap, errString(snap.Detail)
	}
	return snap, nil
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

func fillSizes(ctx context.Context, vols []volumeObs) []volumeObs {
	n := 0
	seen := map[string]bool{}
	for i := range vols {
		if vols[i].SizeBytes > 0 {
			continue
		}
		path := vols[i].Mountpoint
		if path == "" || seen[path] || !allowDu(path) {
			continue
		}
		if n >= 20 || ctx.Err() != nil {
			break
		}
		seen[path] = true
		n++
		out, err := runCmd(ctx, 2*time.Second, "du", "-sx", "-B1", path)
		if err != nil {
			continue
		}
		if sz, ok := parseDu(string(out)); ok {
			vols[i].SizeBytes = sz
		}
	}
	return vols
}

func allowDu(path string) bool {
	path = filepath.Clean(path)
	if path == "" || !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return false
	}
	switch path {
	case "/", "/var", "/usr", "/home", "/opt", "/tmp", "/root":
		return false
	}
	// Require /a/b/c so a top-level directory is not walked.
	if strings.Count(strings.Trim(path, "/"), "/") < 2 {
		return false
	}
	return true
}

func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	return cmd.Output()
}
