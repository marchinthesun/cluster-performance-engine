//go:build linux

package evict

import (
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// CPUTarget is a set of logical CPU ids.
type CPUTarget struct {
	Mask map[int]struct{}
}

func NewCPUTarget(cpus []int32) *CPUTarget {
	m := make(map[int]struct{}, len(cpus))
	for _, c := range cpus {
		m[int(c)] = struct{}{}
	}
	return &CPUTarget{Mask: m}
}

func cpuSetIntersects(set *unix.CPUSet, target *CPUTarget) bool {
	for cpu := range target.Mask {
		if set.IsSet(cpu) {
			return true
		}
	}
	return false
}

// ForeignPIDsOnCPUs lists PIDs (excluding skip) whose affinity intersects target CPUs.
func ForeignPIDsOnCPUs(target *CPUTarget, skip map[int]struct{}) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var out []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 1 || pid == self {
			continue
		}
		if _, sk := skip[pid]; sk {
			continue
		}
		var set unix.CPUSet
		if err := unix.SchedGetaffinity(pid, &set); err != nil {
			continue
		}
		if cpuSetIntersects(&set, target) {
			out = append(out, pid)
		}
	}
	return out, nil
}

// StopForeignOnCPUs sends sig to every PID whose affinity intersects target (best-effort CAP_KILL for other users).
func StopForeignOnCPUs(target *CPUTarget, skip map[int]struct{}, sig unix.Signal, dryRun bool) (int, error) {
	pids, err := ForeignPIDsOnCPUs(target, skip)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, pid := range pids {
		if dryRun {
			n++
			continue
		}
		if err := unix.Kill(pid, sig); err == nil {
			n++
		}
	}
	return n, nil
}

// ThreadPath returns /proc/pid/task entries (Linux).
func ThreadPath(pid int) ([]int, error) {
	pat := filepath.Join("/proc", strconv.Itoa(pid), "task")
	entries, err := os.ReadDir(pat)
	if err != nil {
		return nil, err
	}
	var tids []int
	for _, e := range entries {
		tid, err := strconv.Atoi(e.Name())
		if err == nil {
			tids = append(tids, tid)
		}
	}
	return tids, nil
}
