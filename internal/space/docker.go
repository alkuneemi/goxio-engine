package space

import (
	"context"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

const maxDockerInspect = 60

// readDocker lists containers and volumes through the Docker API (DOCKER_HOST,
// which should be a read-only socket proxy in production). It never calls a
// write endpoint. A missing daemon is an error the caller can ignore.
func readDocker(ctx context.Context, logger *zap.SugaredLogger) ([]volumeObs, []containerObs, error) {
	if logger == nil {
		logger = zap.NewNop().Sugar()
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, nil, err
	}
	defer cli.Close()
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		return nil, nil, err
	}

	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, nil, err
	}

	var vols []volumeObs
	if vres, verr := cli.VolumeList(ctx, client.VolumeListOptions{}); verr != nil {
		logger.Infow("space collector: volume list skipped", "err", verr)
	} else {
		for _, v := range vres.Items {
			obs := volumeObs{Name: v.Name, Mountpoint: v.Mountpoint}
			if v.UsageData != nil && v.UsageData.Size > 0 {
				obs.SizeBytes = v.UsageData.Size
			}
			vols = append(vols, obs)
		}
	}

	var ctrs []containerObs
	for i, ctr := range list.Items {
		if ctx.Err() != nil {
			break
		}
		info := containerFromSummary(ctr)
		if i < maxDockerInspect {
			ins, ierr := cli.ContainerInspect(ctx, ctr.ID, client.ContainerInspectOptions{})
			if ierr != nil {
				logger.Debugw("space collector: inspect skipped", "container", info.Name, "err", ierr)
			} else {
				applyInspect(&info, ins.Container)
			}
		}
		for _, m := range info.Mounts {
			if strings.EqualFold(m.Kind, "bind") && m.Source != "" {
				vols = append(vols, volumeObs{Mountpoint: m.Source})
			}
		}
		ctrs = append(ctrs, info)
	}
	return vols, ctrs, nil
}

func containerFromSummary(ctr container.Summary) containerObs {
	info := containerObs{
		ID:     ctr.ID,
		Name:   containerDisplayName(ctr.Names),
		Image:  ctr.Image,
		Status: string(ctr.State),
		Labels: ctr.Labels,
		Mounts: mountsFrom(ctr.Mounts),
	}
	if info.Name == "" {
		info.Name = shortID(ctr.ID)
	}
	return info
}

func applyInspect(info *containerObs, inspect container.InspectResponse) {
	if inspect.Name != "" {
		info.Name = strings.TrimPrefix(inspect.Name, "/")
	}
	if inspect.Config != nil {
		if inspect.Config.Image != "" {
			info.Image = inspect.Config.Image
		}
		if len(inspect.Config.Labels) > 0 {
			if info.Labels == nil {
				info.Labels = map[string]string{}
			}
			for k, v := range inspect.Config.Labels {
				info.Labels[k] = v
			}
		}
		info.DBName = dbNameFromEnv(inspect.Config.Env)
	}
	if inspect.State != nil {
		if inspect.State.Status != "" {
			info.Status = string(inspect.State.Status)
		}
		if t, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
			info.StartedAt = t
		}
	}
	if len(inspect.Mounts) > 0 {
		info.Mounts = mountsFrom(inspect.Mounts)
	}
	if inspect.SizeRw != nil && *inspect.SizeRw > 0 {
		info.SizeBytes = *inspect.SizeRw
	}
}

func mountsFrom(ms []container.MountPoint) []mountObs {
	out := make([]mountObs, 0, len(ms))
	for _, m := range ms {
		out = append(out, mountObs{
			Kind:   string(m.Type),
			Name:   m.Name,
			Source: m.Source,
			Dest:   m.Destination,
		})
	}
	return out
}

func containerDisplayName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// dbNameFromEnv keeps only database-name variables. Passwords and DSNs are
// never copied onto the map.
func dbNameFromEnv(env []string) string {
	allow := []string{"POSTGRES_DB=", "MYSQL_DATABASE=", "MONGO_INITDB_DATABASE="}
	for _, e := range env {
		for _, p := range allow {
			if strings.HasPrefix(e, p) {
				v := strings.TrimSpace(strings.TrimPrefix(e, p))
				if v == "" || strings.ContainsAny(v, " \t\r\n") {
					continue
				}
				low := strings.ToLower(v)
				if strings.Contains(low, "pass") || strings.Contains(low, "secret") || strings.Contains(v, "://") {
					continue
				}
				return v
			}
		}
	}
	return ""
}
