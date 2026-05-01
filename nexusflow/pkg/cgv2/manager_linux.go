//go:build linux

package cgv2

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Manager owns a cgroup v2 subtree for NexusFlow cells.
type Manager struct {
	Root string
}

func NewManager(root string) (*Manager, error) {
	if root == "" {
		root = "/sys/fs/cgroup/nexusflow-daemon"
	}
	m := &Manager{Root: root}
	if err := os.MkdirAll(m.Root, 0o755); err != nil {
		return nil, err
	}
	parent := filepath.Dir(m.Root)
	// Best-effort: requires write access to parent cgroup.subtree_control (often root).
	_, _ = enableController(parent, "cpuset"), enableController(parent, "memory")
	_ = enableController(m.Root, "cpuset")
	return m, nil
}

func enableController(cgroupDir, name string) error {
	ctl := filepath.Join(cgroupDir, "cgroup.subtree_control")
	data, err := os.ReadFile(ctl)
	if err != nil {
		return err
	}
	if strings.Contains(string(data), name) {
		return nil
	}
	f, err := os.OpenFile(ctl, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "+%s\n", name)
	return err
}

// CellPath returns the cgroup directory for a sanitized cell id.
func (m *Manager) CellPath(cellID string) string {
	return filepath.Join(m.Root, "cell-"+sanitizeID(cellID))
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "default"
	}
	return out
}

// CreateCell configures cpuset.cpus, cpuset.mems, and optional exclusive partition.
func (m *Manager) CreateCell(cellID string, cpus, mems []int32, exclusive bool) (path string, err error) {
	path = m.CellPath(cellID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	cpuStr := joinIntsNonEmpty(cpus)
	if err := os.WriteFile(filepath.Join(path, "cpuset.cpus"), []byte(cpuStr+"\n"), 0); err != nil {
		return "", fmt.Errorf("cpuset.cpus: %w", err)
	}
	var memStr string
	if len(mems) == 0 {
		parentMems, _ := os.ReadFile(filepath.Join(filepath.Dir(path), "cpuset.mems"))
		memStr = strings.TrimSpace(string(parentMems))
		if memStr == "" {
			memStr = "0"
		}
	} else {
		memStr = joinInts(mems, ",")
	}
	if err := os.WriteFile(filepath.Join(path, "cpuset.mems"), []byte(memStr+"\n"), 0); err != nil {
		return "", fmt.Errorf("cpuset.mems: %w", err)
	}
	if exclusive {
		part := filepath.Join(path, "cpuset.cpus.partition")
		_ = os.WriteFile(part, []byte("isolated\n"), 0)
	}
	return path, nil
}

func joinInts(xs []int32, sep string) string {
	if len(xs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, v := range xs {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(strconv.Itoa(int(v)))
	}
	return b.String()
}

func joinIntsNonEmpty(xs []int32) string {
	s := joinInts(xs, ",")
	if s == "" {
		return "0"
	}
	return s
}

// Destroy removes a cell cgroup directory (fails if processes still attached).
func (m *Manager) DestroyCell(cellID string) error {
	path := m.CellPath(cellID)
	return os.RemoveAll(path)
}

// Attach writes pid to cgroup.procs.
func Attach(cgroupPath string, pid int) error {
	return os.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), []byte(strconv.Itoa(pid)+"\n"), 0)
}

// ListPIDs returns processes in a cgroup (best-effort).
func ListPIDs(cgroupPath string) ([]int, error) {
	f, err := os.Open(filepath.Join(cgroupPath, "cgroup.procs"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		pid, err := strconv.Atoi(strings.TrimSpace(sc.Text()))
		if err != nil {
			continue
		}
		out = append(out, pid)
	}
	return out, sc.Err()
}
