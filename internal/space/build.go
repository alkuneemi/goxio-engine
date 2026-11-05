package space

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/guilherme-grimm/graph-go/internal/graph/edges"
	"github.com/guilherme-grimm/graph-go/internal/graph/nodes"
)

type diskObs struct {
	Filesystem string
	FSType     string
	Mount      string
	Size       int64
	Used       int64
	Avail      int64
}

type volumeObs struct {
	Name       string
	Mountpoint string
	Kind       string
	Digest     string
	Tags       []string
	SizeBytes  int64
	SizeState  string // measured, unreadable, timeout, pending, skipped, unknown
	SizeReason string
}

type mountObs struct {
	Kind   string
	Name   string
	Source string
	Dest   string
}

type containerObs struct {
	ID           string
	Name         string
	Image        string
	Status       string
	StartedAt    time.Time
	Labels       map[string]string
	Mounts       []mountObs
	DBName       string
	SizeBytes    int64
	InspectState string // full, partial, unreadable
}

type diskMount struct {
	id    string
	mount string
}

type volRec struct {
	id         string
	name       string
	path       string
	size       int64
	sizeState  string
	sizeReason string
	kind       string
	digest     string
	tags       []string
	parent     string
	containers []string
}

// buildSnapshot turns read-only observations into the space hierarchy.
// It does not touch the host.
func buildSnapshot(host string, now time.Time, disks []diskObs, volList []volumeObs, ctrs []containerObs) Snapshot {
	if host == "" {
		host = "host"
	}
	b := &builder{}

	var mounts []diskMount
	groups := map[string]string{}
	ensureGroup := func(device string) string {
		group := deviceGroup(device)
		if group == "" {
			group = "unknown"
		}
		if id, ok := groups[group]; ok {
			return id
		}
		id := spaceID("disk-group", group)
		groups[group] = id
		md := map[string]any{
			"kind":          "device-group",
			"device_group":  group,
			"cleanable":     false,
			"unused":        false,
			"size_state":    "not-aggregated",
			"accounting":    "mounts-are-separate",
			"physical_disk": false,
		}
		b.addNode(newNode(id, string(nodes.TypeDisk), group, "", "unknown", sourceHost, host, md))
		return id
	}
	seenMount := map[string]bool{}
	for _, d := range disks {
		if d.Mount == "" {
			continue
		}
		key := d.Filesystem + "\x00" + d.Mount
		if seenMount[key] {
			continue
		}
		seenMount[key] = true
		id := spaceID("disk", d.Filesystem+"-"+d.Mount)
		parent := ""
		if d.Filesystem != "" {
			parent = ensureGroup(d.Filesystem)
		}
		pressure := 0.0
		known := d.Size > 0
		if known {
			pressure = float64(d.Used) / float64(d.Size)
		}
		group := deviceGroup(d.Filesystem)
		md := map[string]any{
			"path":         d.Mount,
			"kind":         "mount",
			"device_group": group,
			"fs_type":      d.FSType,
			"cleanable":    false,
			"unused":       false,
			"size_state":   "unknown",
			"accounting":   "filesystem",
		}
		putStr(md, "filesystem", d.Filesystem)
		putStr(md, "device", d.Filesystem)
		if known {
			md["size_state"] = "measured"
			putSize(md, d.Size)
		}
		if d.Used > 0 {
			md["used_bytes"] = d.Used
		}
		putFloat(md, "disk_pressure", pressure, known)
		b.addNode(newNode(id, string(nodes.TypeDisk), diskName(d), parent, healthFromPressure(pressure, known), sourceHost, host, md))
		if parent != "" {
			b.addEdge(newEdge(parent, id, edges.TypeContains, "CONTAINS"))
		}
		mounts = append(mounts, diskMount{id: id, mount: d.Mount})
	}

	vols := map[string]*volRec{}
	ensureVol := func(in volumeObs) *volRec {
		kind := in.Kind
		if kind == "" {
			kind = "volume"
			if in.Name == "" {
				kind = "folder"
			}
		}
		path := in.Mountpoint
		if path != "" {
			path = filepath.Clean(path)
		}
		if kind != "volume" && kind != "image" && path != "" {
			for _, existing := range vols {
				if existing.path != "" && filepath.Clean(existing.path) == path {
					return existing
				}
			}
		}
		material := in.Name
		idKind := "volume"
		switch kind {
		case "image":
			idKind = "image"
			if in.Digest != "" {
				material = in.Digest
			}
		case "folder", "log", "backup", "runtime":
			idKind = "folder"
			material = path
		}
		if material == "" {
			material = path
		}
		if material == "" {
			return nil
		}
		key := idKind + ":" + material
		if v, ok := vols[key]; ok {
			if in.SizeState == "measured" && in.SizeBytes > v.size {
				v.size = in.SizeBytes
				v.sizeState = "measured"
				v.sizeReason = in.SizeReason
			}
			if v.path == "" {
				v.path = path
			}
			for _, tag := range in.Tags {
				v.tags = appendUnique(v.tags, tag)
			}
			if v.digest == "" {
				v.digest = in.Digest
			}
			return v
		}
		state := in.SizeState
		if state == "" {
			if in.SizeBytes > 0 {
				state = "measured"
			} else {
				state = "unknown"
			}
		}
		display := in.Name
		if display == "" {
			display = path
		}
		parentPath := path
		if parentPath == "" {
			parentPath = "/"
		}
		v := &volRec{
			id:         spaceID(idKind, material),
			name:       display,
			path:       path,
			size:       in.SizeBytes,
			sizeState:  state,
			sizeReason: in.SizeReason,
			kind:       kind,
			digest:     in.Digest,
			tags:       append([]string{}, in.Tags...),
			parent:     diskFor(mounts, parentPath),
		}
		vols[key] = v
		return v
	}

	for _, v := range volList {
		ensureVol(v)
	}

	type ctrBuilt struct {
		id       string
		name     string
		activity string
		core     bool
		clean    bool
		unused   bool
		health   string
		repoID   string
		repoName string
		account  string
		volIDs   []string
		parent   string
		size     int64
		path     string
		image    string
		engine   string
		dbName   string
		dbCore   bool
		inspect  string
	}
	var built []ctrBuilt
	repoSeen := map[string]struct {
		id, name, owner string
		activity        string
		containers      []string
	}{}

	for _, c := range ctrs {
		name := c.Name
		if name == "" {
			name = c.ID
		}
		id := spaceID("container", name)
		labels := c.Labels
		engine, imageCore := imageClass(c.Image)
		core := imageCore || labelBool(labels, "goxio.core") || strings.EqualFold(labelGet(labels, "goxio.activity"), ActivityCore)
		activity := activityOf(c.Status, c.StartedAt, core, now)
		if a := strings.ToLower(labelGet(labels, "goxio.activity")); activityRank(a) > 0 && !core {
			activity = a
		}
		if core {
			activity = ActivityCore
		}
		st := strings.ToLower(c.Status)
		unused := !core && st != "running" && st != "paused"
		clean := unused
		if labelBool(labels, "goxio.cleanable") {
			clean = !core
		}
		if v, ok := labels["goxio.cleanable"]; ok && (v == "false" || v == "0") {
			clean = false
		}

		var mounted []*volRec
		for _, m := range c.Mounts {
			kind := strings.ToLower(m.Kind)
			if kind == "tmpfs" || kind == "npipe" {
				continue
			}
			var v *volRec
			if kind == "volume" || m.Name != "" {
				v = ensureVol(volumeObs{Name: m.Name, Mountpoint: m.Source, Kind: "volume"})
			} else if m.Source != "" {
				v = ensureVol(volumeObs{Mountpoint: m.Source, Kind: "folder"})
			}
			if v == nil {
				continue
			}
			v.containers = appendUnique(v.containers, name)
			mounted = append(mounted, v)
		}

		parent := ""
		var volIDs []string
		var best *volRec
		for _, v := range mounted {
			volIDs = append(volIDs, v.id)
			if best == nil || v.size > best.size {
				best = v
			}
		}
		if best != nil {
			parent = best.id
		} else if len(mounts) > 0 {
			parent = diskFor(mounts, "/")
		}

		owner, repo, override := repoFromLabels(labels)
		if override != "" && repo != "" {
			owner = override
		}
		if repo == "" {
			owner = ""
		}
		repoID, repoName := "", ""
		if repo != "" && owner != "" {
			repoID = spaceID("repo", owner+"-"+repo)
			repoName = repo
			rec := repoSeen[repoID]
			rec.id = repoID
			rec.name = repoName
			rec.owner = owner
			rec.activity = hotter(rec.activity, activity)
			rec.containers = appendUnique(rec.containers, name)
			repoSeen[repoID] = rec
		}

		size := c.SizeBytes
		path := ""
		if best != nil {
			path = best.path
			if best.size > size {
				size = best.size
			}
		}
		dbName := c.DBName
		if dbName == "" {
			dbName = labelGet(labels, "goxio.database")
		}
		built = append(built, ctrBuilt{
			id: id, name: name, activity: activity, core: core, clean: clean, unused: unused,
			health: containerHealth(st), repoID: repoID, repoName: repoName, account: owner,
			volIDs: volIDs, parent: parent, size: size, path: path, image: c.Image,
			engine: engine, dbName: dbName, dbCore: core || isTemplateDB(dbName),
			inspect: c.InspectState,
		})
	}

	// Volumes after containers so unused/activity see the links.
	for _, v := range vols {
		activity := ActivityIdle
		unused := len(v.containers) == 0
		clean := unused
		health := "unknown"
		if !unused {
			health = "healthy"
			clean = true
			for _, c := range built {
				linked := false
				for _, id := range c.volIDs {
					if id == v.id {
						linked = true
						break
					}
				}
				if !linked {
					continue
				}
				activity = hotter(activity, c.activity)
				if !c.clean {
					clean = false
				}
			}
			if activity == ActivityCore {
				clean = false
				unused = false
			}
		}
		state := v.sizeState
		if state == "" {
			if v.size > 0 {
				state = "measured"
			} else {
				state = "unknown"
			}
		}
		md := map[string]any{
			"kind":            v.kind,
			"cleanable":       clean,
			"unused":          unused,
			"activity":        activity,
			"containers":      v.containers,
			"size_state":      state,
			"overlaps_parent": true,
		}
		putStr(md, "path", v.path)
		putStr(md, "size_reason", v.sizeReason)
		putStr(md, "digest", v.digest)
		if v.kind == "volume" {
			putStr(md, "volume_name", v.name)
		}
		if len(v.tags) > 0 {
			md["tags"] = append([]string{}, v.tags...)
		}
		if state == "measured" {
			putSize(md, v.size)
		}
		b.addNode(newNode(v.id, string(nodes.TypeVolume), v.name, v.parent, health, sourceHost, host, md))
		if v.parent != "" {
			b.addEdge(newEdge(v.parent, v.id, edges.TypeContains, "CONTAINS"))
		}
	}

	for _, c := range built {
		provenance := "unknown"
		if c.repoID != "" {
			provenance = "label"
		}
		md := map[string]any{
			"activity":   c.activity,
			"cleanable":  c.clean,
			"unused":     c.unused,
			"core":       c.core,
			"containers": []string{c.name},
			"provenance": provenance,
		}
		putStr(md, "path", c.path)
		putStr(md, "image", c.image)
		putStr(md, "inspect_state", c.inspect)
		putStr(md, "repo", c.repoID)
		putStr(md, "repo_name", c.repoName)
		if c.repoID != "" {
			putStr(md, "github_account", c.account)
		}
		putStr(md, "engine", c.engine)
		putSize(md, c.size)
		b.addNode(newNode(c.id, string(nodes.TypeContainer), c.name, c.parent, c.health, sourceHost, host, md))
		if c.parent != "" {
			b.addEdge(newEdge(c.parent, c.id, edges.TypeContains, "CONTAINS"))
		}
		for _, vid := range c.volIDs {
			if vid != c.parent {
				b.addEdge(newEdge(vid, c.id, edges.TypeContains, "CONTAINS"))
			}
		}
		if c.repoID != "" {
			b.addEdge(newEdge(c.repoID, c.id, edges.TypeDeploysFrom, "DEPLOYS_FROM"))
		}
		if c.dbName != "" {
			dbID := spaceID("database", c.name+"-"+c.dbName)
			dbActivity := c.activity
			dbCore := c.dbCore
			if dbCore {
				dbActivity = ActivityCore
			}
			dbUnused := c.unused && !dbCore
			dbClean := dbUnused && !dbCore
			dmd := map[string]any{
				"activity":   dbActivity,
				"cleanable":  dbClean,
				"unused":     dbUnused,
				"core":       dbCore,
				"containers": []string{c.name},
			}
			putStr(dmd, "path", c.path)
			putStr(dmd, "repo", c.repoID)
			putStr(dmd, "repo_name", c.repoName)
			putStr(dmd, "github_account", c.account)
			putStr(dmd, "engine", c.engine)
			b.addNode(newNode(dbID, string(nodes.TypeDatabase), c.dbName, c.id, c.health, sourceHost, host, dmd))
			b.addEdge(newEdge(c.id, dbID, edges.TypeContains, "CONTAINS"))
		}
	}

	accounts := map[string]string{}
	for _, rec := range repoSeen {
		if rec.owner == "" {
			continue
		}
		acctID := spaceID("github_account", rec.owner)
		accounts[rec.owner] = acctID
		unused := len(rec.containers) == 0
		activity := rec.activity
		if activity == "" {
			activity = ActivityIdle
		}
		md := map[string]any{
			"activity":       activity,
			"cleanable":      unused && activity != ActivityCore,
			"unused":         unused,
			"repo":           rec.id,
			"repo_name":      rec.name,
			"github_account": rec.owner,
			"containers":     rec.containers,
		}
		b.addNode(newNode(rec.id, string(nodes.TypeRepo), rec.name, "", "healthy", sourceHost, host, md))
		b.addEdge(newEdge(acctID, rec.id, edges.TypeOwns, "OWNS"))
	}
	for login, id := range accounts {
		md := map[string]any{
			"cleanable":      false,
			"unused":         false,
			"github_account": login,
		}
		b.addNode(newNode(id, string(nodes.TypeGitHubAccount), login, "", "healthy", sourceHost, host, md))
	}

	return finalize(Snapshot{
		CollectedAt: now.UTC(),
		Host:        host,
		Source:      sourceHost,
		HostWrites:  false,
		Nodes:       b.nodes,
		Edges:       b.edges,
	})
}

type builder struct {
	nodes    []nodes.Node
	edges    []edges.Edge
	seenNode map[string]bool
	seenEdge map[string]bool
}

func (b *builder) addNode(n nodes.Node) {
	if b.seenNode == nil {
		b.seenNode = map[string]bool{}
	}
	if n.Id == "" || b.seenNode[n.Id] {
		return
	}
	b.seenNode[n.Id] = true
	b.nodes = append(b.nodes, n)
}

func (b *builder) addEdge(e edges.Edge) {
	if b.seenEdge == nil {
		b.seenEdge = map[string]bool{}
	}
	if e.Source == "" || e.Target == "" || e.Source == e.Target || b.seenEdge[e.Id] {
		return
	}
	b.seenEdge[e.Id] = true
	b.edges = append(b.edges, e)
}

func diskName(d diskObs) string {
	if d.Filesystem != "" && d.Mount != "" {
		return d.Filesystem + " " + d.Mount
	}
	if d.Mount != "" {
		return d.Mount
	}
	return d.Filesystem
}

func diskFor(mounts []diskMount, path string) string {
	if path == "" {
		path = "/"
	}
	path = filepath.Clean(path)
	bestLen := -1
	best := ""
	for _, d := range mounts {
		m := filepath.Clean(d.mount)
		if m == "/" || path == m || strings.HasPrefix(path, strings.TrimRight(m, "/")+"/") {
			if len(m) > bestLen {
				bestLen = len(m)
				best = d.id
			}
		}
	}
	return best
}

func healthFromPressure(p float64, known bool) string {
	if !known {
		return "unknown"
	}
	if p >= 0.9 {
		return "unhealthy"
	}
	if p >= 0.75 {
		return "degraded"
	}
	return "healthy"
}

func containerHealth(status string) string {
	switch status {
	case "running":
		return "healthy"
	case "paused":
		return "degraded"
	case "restarting", "dead":
		return "unhealthy"
	default:
		return "unknown"
	}
}

func activityOf(status string, started time.Time, core bool, now time.Time) string {
	if core {
		return ActivityCore
	}
	st := strings.ToLower(status)
	if st != "running" && st != "paused" {
		return ActivityIdle
	}
	if started.IsZero() || now.IsZero() {
		return ActivityDaily
	}
	age := now.Sub(started)
	switch {
	case age < 24*time.Hour:
		return ActivityDaily
	case age < 7*24*time.Hour:
		return ActivityIntermittent
	default:
		return ActivityIdle
	}
}

func imageClass(image string) (engine string, core bool) {
	n := strings.ToLower(image)
	switch {
	case strings.Contains(n, "postgres"):
		return "postgres", true
	case strings.Contains(n, "mariadb"), strings.Contains(n, "mysql"):
		return "mysql", true
	case strings.Contains(n, "mongo"):
		return "mongodb", true
	case strings.Contains(n, "redis"):
		return "redis", true
	case strings.Contains(n, "elastic"):
		return "elasticsearch", true
	default:
		return "", false
	}
}

func isTemplateDB(name string) bool {
	switch strings.ToLower(name) {
	case "template0", "template1", "postgres":
		return true
	default:
		return false
	}
}

func labelGet(labels map[string]string, key string) string {
	if labels == nil {
		return ""
	}
	return strings.TrimSpace(labels[key])
}

func labelBool(labels map[string]string, key string) bool {
	v := strings.ToLower(labelGet(labels, key))
	return v == "1" || v == "true" || v == "yes"
}

func repoFromLabels(labels map[string]string) (owner, repo, accountOverride string) {
	accountOverride = labelGet(labels, "goxio.github")
	raw := labelGet(labels, "goxio.repo")
	if raw == "" {
		raw = labelGet(labels, "org.opencontainers.image.source")
	}
	if raw == "" {
		raw = labelGet(labels, "org.opencontainers.image.url")
	}
	owner, repo, _ = parseGitHub(raw)
	if accountOverride != "" && owner == "" {
		owner = accountOverride
	}
	return owner, repo, accountOverride
}

func parseGitHub(raw string) (owner, repo string, ok bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	raw = strings.TrimPrefix(raw, "git@github.com:")
	raw = strings.TrimPrefix(raw, "ssh://git@github.com/")
	for _, p := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		if strings.HasPrefix(strings.ToLower(raw), p) {
			raw = raw[len(p):]
			break
		}
	}
	raw = strings.Trim(raw, "/")
	parts := strings.Split(raw, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if strings.Contains(parts[0], " ") || strings.Contains(parts[1], " ") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	return append(ss, v)
}
