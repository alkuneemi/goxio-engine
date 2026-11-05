package space

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ErrSizeUnreadable means the path cannot be read. It is not a zero-byte tree.
var ErrSizeUnreadable = errors.New("unreadable")

// SizeNode is one directory in a bounded sizing walk. Bytes on a partial node
// are only the finished children so far, not a total.
type SizeNode struct {
	Name     string     `json:"name,omitempty"`
	State    string     `json:"state,omitempty"`
	Bytes    int64      `json:"bytes,omitempty"`
	Known    bool       `json:"known,omitempty"`
	Progress string     `json:"progress,omitempty"`
	Children []SizeNode `json:"children,omitempty"`
}

type sizeEntry struct {
	Name  string
	Kind  string // dir, file, mount, secret, unreadable
	Bytes int64
}

type sizeWalker interface {
	Du(ctx context.Context, path string) (int64, error)
	List(ctx context.Context, path string) ([]sizeEntry, error)
}

// advanceSizes spends at most budget du/list steps across paths that are not
// finished. A later call with the same SizeWork map continues inside a
// directory that timed out instead of starting that walk over.
func advanceSizes(ctx context.Context, vols []volumeObs, work map[string]SizeNode, budget int, w sizeWalker) ([]volumeObs, map[string]SizeNode, []string) {
	out := append([]volumeObs{}, vols...)
	if work == nil {
		work = map[string]SizeNode{}
	}
	if budget < 0 {
		budget = 0
	}
	for budget > 0 {
		if ctx.Err() != nil {
			break
		}
		moved := false
		for i := range out {
			if budget <= 0 || ctx.Err() != nil {
				break
			}
			path := filepath.Clean(out[i].Mountpoint)
			if path == "" || path == "/" || path == "." {
				continue
			}
			if out[i].Kind == "image" || out[i].SizeState == "measured" || out[i].SizeState == "skipped" || out[i].SizeState == "unreadable" {
				continue
			}
			tree := work[path]
			if tree.State == "measured" || tree.State == "unreadable" {
				applySizeTree(&out[i], tree)
				continue
			}
			next, spent := stepSize(ctx, path, tree, w)
			if !spent {
				applySizeTree(&out[i], next)
				work[path] = next
				continue
			}
			budget--
			moved = true
			work[path] = next
			applySizeTree(&out[i], next)
		}
		if !moved {
			break
		}
	}
	return out, work, nil
}

func stepSize(ctx context.Context, path string, n SizeNode, w sizeWalker) (SizeNode, bool) {
	if n.State == "measured" || n.State == "unreadable" || n.State == "skipped_mount" || n.State == "skipped_secret" {
		return n, false
	}
	if len(n.Children) == 0 {
		if ctx.Err() != nil {
			n.State = "pending"
			return n, false
		}
		sz, err := w.Du(ctx, path)
		if err == nil {
			n.State = "measured"
			n.Bytes = sz
			n.Known = true
			n.Progress = "measured"
			return n, true
		}
		if errors.Is(err, ErrSizeUnreadable) || os.IsPermission(err) {
			n.State = "unreadable"
			n.Known = false
			n.Bytes = 0
			n.Progress = "unreadable"
			return n, true
		}
		ents, listErr := w.List(ctx, path)
		if listErr != nil {
			if errors.Is(listErr, ErrSizeUnreadable) || os.IsPermission(listErr) {
				n.State = "unreadable"
				n.Known = false
				n.Bytes = 0
				n.Progress = "unreadable"
				return n, true
			}
			n.State = "timeout"
			n.Progress = "timed out before children could be listed"
			return n, true
		}
		n.Children = entriesToNodes(ents)
		rollupSize(&n)
		return n, true
	}
	for i := range n.Children {
		c := n.Children[i]
		if c.State == "measured" || c.State == "unreadable" || c.State == "skipped_mount" || c.State == "skipped_secret" {
			continue
		}
		if c.Name == "" {
			continue
		}
		updated, spent := stepSize(ctx, filepath.Join(path, c.Name), c, w)
		n.Children[i] = updated
		rollupSize(&n)
		return n, spent
	}
	rollupSize(&n)
	return n, false
}

func entriesToNodes(ents []sizeEntry) []SizeNode {
	out := make([]SizeNode, 0, len(ents))
	for _, e := range ents {
		switch e.Kind {
		case "file":
			out = append(out, SizeNode{Name: e.Name, State: "measured", Bytes: e.Bytes, Known: true, Progress: "file"})
		case "mount":
			out = append(out, SizeNode{Name: e.Name, State: "skipped_mount", Progress: "other mount"})
		case "secret":
			out = append(out, SizeNode{Name: "(redacted)", State: "skipped_secret", Progress: "secret path omitted"})
		case "unreadable":
			out = append(out, SizeNode{Name: e.Name, State: "unreadable", Progress: "unreadable"})
		default:
			out = append(out, SizeNode{Name: e.Name, State: "pending"})
		}
	}
	return out
}

func rollupSize(n *SizeNode) {
	if len(n.Children) == 0 {
		return
	}
	var sum int64
	measured, unknown, waiting := 0, 0, 0
	for _, c := range n.Children {
		switch c.State {
		case "measured":
			measured++
			sum += c.Bytes
		case "skipped_mount", "skipped_secret":
			measured++
		case "unreadable":
			measured++
			unknown++
		case "partial":
			waiting++
			sum += c.Bytes
		default:
			waiting++
		}
	}
	n.Bytes = sum
	n.Known = measured > 0 || unknown > 0
	n.Progress = fmt.Sprintf("%d/%d done, %d unreadable", measured, len(n.Children), unknown)
	switch {
	case waiting == 0 && unknown == 0:
		n.State = "measured"
		n.Progress = "measured"
	case waiting == 0 && unknown > 0 && measured == unknown:
		n.State = "unreadable"
		n.Known = false
		n.Bytes = 0
		n.Progress = "unreadable"
	default:
		n.State = "partial"
	}
}

func applySizeTree(v *volumeObs, n SizeNode) {
	v.SizeProgress = n.Progress
	switch n.State {
	case "measured":
		v.SizeState = "measured"
		v.SizeBytes = n.Bytes
		v.SizeSoFar = n.Bytes
		v.SizeReason = "du"
	case "unreadable":
		v.SizeState = "unreadable"
		v.SizeBytes = 0
		v.SizeSoFar = 0
		v.SizeReason = "unreadable"
	case "partial":
		v.SizeState = "partial"
		v.SizeBytes = 0
		v.SizeSoFar = n.Bytes
		v.SizeReason = n.Progress
	case "timeout":
		v.SizeState = "timeout"
		v.SizeBytes = 0
		v.SizeSoFar = n.Bytes
		v.SizeReason = n.Progress
	default:
		if n.State != "" {
			v.SizeState = n.State
		}
	}
}

type hostWalker struct {
	root string
}

func (h hostWalker) Du(ctx context.Context, path string) (int64, error) {
	return duBytes(ctx, h.root, path, 2*time.Second)
}

func (h hostWalker) List(ctx context.Context, path string) ([]sizeEntry, error) {
	target := collectorPath(h.root, path)
	parentDev, err := deviceID(target)
	if err != nil {
		return nil, sizeErr(err)
	}
	ents, err := os.ReadDir(target)
	if err != nil {
		return nil, sizeErr(err)
	}
	out := make([]sizeEntry, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		childHost := filepath.Join(path, name)
		childTarget := filepath.Join(target, name)
		if _, secret := sensitivePath(childHost); secret {
			out = append(out, sizeEntry{Kind: "secret"})
			continue
		}
		info, err := os.Lstat(childTarget)
		if err != nil {
			out = append(out, sizeEntry{Name: name, Kind: "unreadable"})
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			out = append(out, sizeEntry{Name: name, Kind: "file", Bytes: info.Size()})
			continue
		}
		dev, devErr := deviceID(childTarget)
		if devErr == nil && parentDev != 0 && dev != 0 && dev != parentDev {
			out = append(out, sizeEntry{Name: name, Kind: "mount"})
			continue
		}
		if info.IsDir() {
			out = append(out, sizeEntry{Name: name, Kind: "dir"})
			continue
		}
		out = append(out, sizeEntry{Name: name, Kind: "file", Bytes: info.Size()})
	}
	return out, nil
}

func collectorPath(root, hostPath string) string {
	if root == "" || root == "/" || root == "." {
		return hostPath
	}
	if hostPath == "/" {
		return root
	}
	return filepath.Join(root, stringsTrimPrefixSlash(hostPath))
}

func stringsTrimPrefixSlash(p string) string {
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	return p
}

func deviceID(path string) (uint64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return 0, nil
	}
	return uint64(st.Dev), nil
}

func sizeErr(err error) error {
	if err == nil {
		return nil
	}
	if os.IsPermission(err) {
		return ErrSizeUnreadable
	}
	return err
}
