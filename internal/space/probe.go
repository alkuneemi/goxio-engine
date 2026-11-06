package space

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
	"github.com/guilherme-grimm/graph-go/internal/redact"
)

// deviceGroup collapses partition names onto one device family.
// /dev/sda1, /dev/sda15 and /dev/sda16 share "sda". LVM mapper devices stay
// distinct. This is an alias, not a claim that the kernel disk size was summed.
func deviceGroup(device string) string {
	d := strings.TrimPrefix(strings.TrimSpace(device), "/dev/")
	if d == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(d, "mapper/"); ok && rest != "" {
		return "mapper/" + rest
	}
	if strings.HasPrefix(d, "nvme") {
		if i := strings.LastIndex(d, "p"); i > 0 && digits(d[i+1:]) {
			return d[:i]
		}
	}
	i := len(d)
	for i > 0 && d[i-1] >= '0' && d[i-1] <= '9' {
		i--
	}
	if i > 0 && i < len(d) {
		return d[:i]
	}
	return d
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// RewriteHostRoot strips a collector bind mount (for example /host-disk)
// so node paths are the host paths. Unrelated paths are unchanged.
func RewriteHostRoot(path, root string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." || root == "/" || path == "" {
		return path
	}
	if path == root {
		return "/"
	}
	if strings.HasPrefix(path, root+"/") {
		rest := strings.TrimPrefix(path, root)
		if rest == "" {
			return "/"
		}
		return rest
	}
	return path
}

// enrichContainers inspects every listed container. A deadline or a single
// inspect error keeps the container and marks it partial. Nothing is dropped.
func enrichContainers(ctx context.Context, listed []containerObs, inspect func(context.Context, containerObs) (containerObs, error)) ([]containerObs, []string) {
	out := make([]containerObs, len(listed))
	var reasons []string
	stopped := false
	for i, c := range listed {
		if stopped || ctx.Err() != nil {
			stopped = true
			c.InspectState = "partial"
			out[i] = c
			continue
		}
		got, err := inspect(ctx, c)
		if err != nil {
			if ctx.Err() != nil {
				stopped = true
				c.InspectState = "partial"
				out[i] = c
				continue
			}
			c.InspectState = "unreadable"
			out[i] = c
			continue
		}
		if got.InspectState == "" {
			got.InspectState = "full"
		}
		out[i] = got
	}
	partial := 0
	for _, c := range out {
		if c.InspectState == "partial" || c.InspectState == "unreadable" {
			partial++
		}
	}
	if partial > 0 {
		reasons = append(reasons, fmt.Sprintf("inspect incomplete: %d of %d containers", partial, len(out)))
	}
	return out, reasons
}

// probeSizes measures at most budget paths starting at cursor and wraps once.
// Unattempted paths stay pending. Timeouts and permission errors are recorded
// on the node; the path is not removed.
func probeSizes(ctx context.Context, vols []volumeObs, budget, cursor int, run func(context.Context, string) (int64, error)) ([]volumeObs, int, []string) {
	out := append([]volumeObs{}, vols...)
	if budget < 0 {
		budget = 0
	}
	n := len(out)
	if n == 0 {
		return out, 0, nil
	}
	if cursor < 0 || cursor >= n {
		cursor = 0
	}
	attempts := 0
	var reasons []string
	var jobs []sizeJob
	seen := 0
	i := cursor
	for seen < n && (budget == 0 || attempts < budget) {
		if ctx.Err() != nil {
			reasons = append(reasons, "size probe stopped: context done")
			break
		}
		idx := i
		v := out[idx]
		i = (i + 1) % n
		seen++
		if !needsSize(v) {
			continue
		}
		path := filepath.Clean(v.Mountpoint)
		if secret, ok := sensitivePath(path); ok {
			v.Mountpoint = secret
			v.SizeBytes = 0
			v.SizeState = "skipped"
			v.SizeReason = "secret_path"
			out[idx] = v
			continue
		}
		if path == "/" || path == "" || path == "." {
			v.SizeState = "skipped"
			v.SizeReason = "covered_by_df"
			out[idx] = v
			continue
		}
		attempts++
		jobs = append(jobs, sizeJob{idx: idx, path: path})
	}
	runSizeJobs(ctx, out, jobs, run)
	pending := 0
	bad := 0
	for _, v := range out {
		switch v.SizeState {
		case "pending", "":
			if needsSize(v) {
				pending++
			}
		case "timeout", "unreadable":
			bad++
		}
	}
	if pending > 0 {
		reasons = append(reasons, fmt.Sprintf("size probe paused: %d paths still pending", pending))
	}
	if bad > 0 {
		reasons = append(reasons, fmt.Sprintf("size probe incomplete: %d paths timeout or unreadable", bad))
	}
	next := i
	return out, next, reasons
}

type sizeJob struct {
	idx  int
	path string
}

func runSizeJobs(ctx context.Context, out []volumeObs, jobs []sizeJob, run func(context.Context, string) (int64, error)) {
	if len(jobs) == 0 {
		return
	}
	workers := duWorkers()
	if workers > len(jobs) {
		workers = len(jobs)
	}
	ch := make(chan sizeJob)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range ch {
				if ctx.Err() != nil {
					continue
				}
				sz, err := run(ctx, job.path)
				mu.Lock()
				v := out[job.idx]
				if err != nil {
					if isTimeout(err) {
						v.SizeState = "timeout"
						v.SizeReason = "timeout"
					} else {
						v.SizeState = "unreadable"
						v.SizeReason = "unreadable"
					}
					v.SizeBytes = 0
				} else {
					v.SizeBytes = sz
					v.SizeState = "measured"
					v.SizeReason = "du"
				}
				out[job.idx] = v
				mu.Unlock()
			}
		}()
	}
	for _, job := range jobs {
		if ctx.Err() != nil {
			break
		}
		ch <- job
	}
	close(ch)
	wg.Wait()
}

func duWorkers() int {
	raw := strings.TrimSpace(os.Getenv("GOXIO_DU_WORKERS"))
	if raw == "" {
		return 2
	}
	n := 0
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 2
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

func needsSize(v volumeObs) bool {
	if v.Mountpoint == "" {
		return false
	}
	switch v.SizeState {
	case "measured", "skipped":
		return false
	case "":
		return v.SizeBytes <= 0
	default:
		return true
	}
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") || strings.Contains(msg, "signal: killed")
}

func sensitivePath(path string) (string, bool) {
	low := strings.ToLower(filepath.Clean(path))
	needles := []string{
		"/.ssh", "/id_rsa", "/id_ed25519", "/id_ecdsa", "/id_dsa",
		"authorized_keys", ".pem", "/etc/shadow", "/etc/gshadow",
		"credentials", ".env", ".kube", "id_rsa", "id_ed25519",
	}
	for _, n := range needles {
		if strings.Contains(low, n) {
			dir := filepath.Dir(path)
			if dir == "." || dir == "" {
				dir = "/"
			}
			return filepath.Clean(dir) + "/(redacted)", true
		}
	}
	return path, false
}

// PathRoots are generic directories a host map should show even when du
// cannot finish. Customer-specific paths are not hard-coded.
func PathRoots() []string {
	return []string{
		"/var", "/var/lib", "/var/lib/docker", "/var/lib/containerd", "/var/log", "/var/backups",
		"/srv", "/opt", "/root", "/data", "/home", "/usr", "/tmp", "/boot", "/snap",
	}
}

func pathKind(path string) string {
	switch filepath.Clean(path) {
	case "/var/log":
		return "log"
	case "/var/backups", "/srv":
		return "backup"
	case "/var/lib/docker", "/var/lib/containerd":
		return "runtime"
	default:
		return "folder"
	}
}

func finalize(s Snapshot) Snapshot {
	s = scrubSnapshot(s)
	s = FillInventory(s)
	if s.TruncatedCount > 0 {
		s.PartialReasons = append(s.PartialReasons, fmt.Sprintf("inspect incomplete: %d of %d containers", s.TruncatedCount, s.ListedContainers))
	}
	if s.SizesPending > 0 || s.SizesTimeout > 0 || s.SizesUnreadable > 0 || s.SizesPartial > 0 {
		s.PartialReasons = append(s.PartialReasons, fmt.Sprintf("sizes measured=%d pending=%d partial=%d timeout=%d unreadable=%d", s.SizesMeasured, s.SizesPending, s.SizesPartial, s.SizesTimeout, s.SizesUnreadable))
	}
	s.Partial = len(s.PartialReasons) > 0
	s.Complete = s.ListingComplete && s.SizesPending == 0 && s.SizesPartial == 0 && s.SizesTimeout == 0 && s.SizesUnreadable == 0
	return s
}

// FillInventory counts nodes by what they are. Images are not volumes.
// A timeout is not a silent gap: it is counted on its own.
func FillInventory(s Snapshot) Snapshot {
	s.ListedContainers = 0
	s.InspectedContainers = 0
	s.TruncatedCount = 0
	s.Volumes = 0
	s.Images = 0
	s.Folders = 0
	s.Mounts = 0
	s.Databases = 0
	s.Repos = 0
	s.Accounts = 0
	s.SizesMeasured = 0
	s.SizesPending = 0
	s.SizesTimeout = 0
	s.SizesUnreadable = 0
	s.SizesPartial = 0
	for _, n := range s.Nodes {
		kind, _ := n.Metadata["kind"].(string)
		switch n.Type {
		case string(nodes.TypeContainer):
			s.ListedContainers++
			st, _ := n.Metadata["inspect_state"].(string)
			if st == "partial" || st == "unreadable" {
				s.TruncatedCount++
			} else {
				s.InspectedContainers++
			}
		case string(nodes.TypeDatabase):
			s.Databases++
		case string(nodes.TypeRepo):
			s.Repos++
		case string(nodes.TypeGitHubAccount):
			s.Accounts++
		case string(nodes.TypeDisk):
			if kind == "mount" {
				s.Mounts++
			}
		case string(nodes.TypeVolume):
			switch kind {
			case "image":
				s.Images++
			case "folder", "log", "backup", "runtime":
				s.Folders++
			default:
				s.Volumes++
			}
		}
		switch sizeStateOf(n) {
		case "measured":
			s.SizesMeasured++
		case "timeout":
			s.SizesTimeout++
		case "unreadable":
			s.SizesUnreadable++
		case "partial":
			s.SizesPartial++
		case "pending", "unknown":
			if countsAsMissingSize(n) {
				s.SizesPending++
			}
		}
	}
	s.MissingSizeCount = s.SizesPending + s.SizesTimeout + s.SizesUnreadable + s.SizesPartial
	s.ListingComplete = s.TruncatedCount == 0
	s.Complete = s.ListingComplete && s.MissingSizeCount == 0
	if !s.Complete {
		s.Partial = true
	}
	return s
}

func sizeStateOf(n nodes.Node) string {
	if n.Metadata == nil {
		return ""
	}
	st, _ := n.Metadata["size_state"].(string)
	return st
}

// MergeSizeProgress keeps a measured size when a newer snapshot lists the same
// node but did not measure it again. Membership follows the newer snapshot.
func MergeSizeProgress(prev, next Snapshot) Snapshot {
	if len(next.SizeWork) == 0 && len(prev.SizeWork) > 0 {
		next.SizeWork = prev.SizeWork
	}
	if len(prev.Nodes) == 0 {
		return next
	}
	old := make(map[string]nodes.Node, len(prev.Nodes))
	for _, n := range prev.Nodes {
		old[n.Id] = n
	}
	for i, n := range next.Nodes {
		was, ok := old[n.Id]
		if !ok || sizeStateOf(was) != "measured" || sizeStateOf(n) == "measured" {
			continue
		}
		if n.Metadata == nil {
			n.Metadata = map[string]any{}
		}
		for _, k := range []string{"size", "size_bytes", "used_bytes", "size_state", "size_reason"} {
			if v, ok := was.Metadata[k]; ok {
				n.Metadata[k] = v
			}
		}
		next.Nodes[i] = n
	}
	return FillInventory(next)
}

func countsAsMissingSize(n nodes.Node) bool {
	if n.Metadata == nil {
		return false
	}
	kind, _ := n.Metadata["kind"].(string)
	if n.Type != string(nodes.TypeVolume) && !(n.Type == string(nodes.TypeDisk) && kind == "mount") {
		return false
	}
	st, _ := n.Metadata["size_state"].(string)
	switch st {
	case "measured", "not-aggregated", "skipped":
		return false
	default:
		return true
	}
}

func scrubSnapshot(s Snapshot) Snapshot {
	for i := range s.Nodes {
		s.Nodes[i] = scrubNode(s.Nodes[i])
	}
	return s
}

func scrubNode(n nodes.Node) nodes.Node {
	if n.Metadata == nil {
		n.Metadata = map[string]any{}
	}
	if p, ok := n.Metadata["path"].(string); ok {
		if red, secret := sensitivePath(p); secret {
			n.Metadata["path"] = red
			n.Metadata["secret_path"] = true
			if n.Name == p || strings.Contains(strings.ToLower(n.Name), "id_rsa") || strings.Contains(strings.ToLower(n.Name), ".ssh") {
				n.Name = red
			}
		}
	}
	n.Name = redact.String(n.Name)
	for _, k := range []string{"ingest_token", "token", "ssh_key", "private_key", "authorization", "password", "account_id", "tenant_id"} {
		delete(n.Metadata, k)
	}
	n.Metadata = redact.Map(n.Metadata)
	n.Metadata["host_writes"] = false
	return n
}

// DuBudget is how many size probes one collect pass may start.
// GOXIO_DU_BUDGET overrides it. Zero means the default.
func DuBudget(env string) int {
	if env == "" {
		return 40
	}
	n := 0
	for _, r := range strings.TrimSpace(env) {
		if r < '0' || r > '9' {
			return 40
		}
		n = n*10 + int(r-'0')
		if n > 10000 {
			return 10000
		}
	}
	if n <= 0 {
		return 40
	}
	return n
}

// InspectTimeout bounds one container inspect so a slow daemon cannot stall
// the whole collect. The list itself is never truncated to a silent cap.
func InspectTimeout() time.Duration { return 8 * time.Second }
