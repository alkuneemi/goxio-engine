package space

import (
	"bufio"
	"strconv"
	"strings"
)

// skipFSTypes are virtual filesystems df reports that are not server disks.
var skipFSTypes = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "squashfs": true, "overlay": true,
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true,
	"devpts": true, "securityfs": true, "pstore": true, "bpf": true,
	"tracefs": true, "debugfs": true, "hugetlbfs": true, "mqueue": true,
	"fusectl": true, "configfs": true, "ramfs": true, "nsfs": true,
	"autofs": true, "binfmt_misc": true, "rpc_pipefs": true, "nfsd": true,
	"efivarfs": true, "fuse.portal": true,
}

// parseDF reads `df -B1 -PT` output. It is a pure parser: nothing is executed.
func parseDF(out string) []diskObs {
	var disks []diskObs
	sc := bufio.NewScanner(strings.NewReader(out))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "Filesystem") {
				continue
			}
		}
		fields := strings.Fields(line)
		// Filesystem Type 1B-blocks Used Available Capacity Mounted on
		if len(fields) < 7 {
			continue
		}
		fsType := fields[1]
		if skipFSTypes[fsType] {
			continue
		}
		mount := strings.Join(fields[6:], " ")
		if mount == "" || strings.HasPrefix(mount, "/proc") || strings.HasPrefix(mount, "/sys") || strings.HasPrefix(mount, "/dev") || strings.HasPrefix(mount, "/run") {
			continue
		}
		size, errS := strconv.ParseInt(fields[2], 10, 64)
		used, errU := strconv.ParseInt(fields[3], 10, 64)
		avail, errA := strconv.ParseInt(fields[4], 10, 64)
		if errS != nil || errU != nil || errA != nil || size <= 0 {
			continue
		}
		disks = append(disks, diskObs{
			Filesystem: fields[0],
			FSType:     fsType,
			Mount:      mount,
			Size:       size,
			Used:       used,
			Avail:      avail,
		})
	}
	return disks
}

func parseDu(out string) (int64, bool) {
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
