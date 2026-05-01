//go:build linux

package topology

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const sysfsCPU = "/sys/devices/system/cpu"
const sysfsNode = "/sys/devices/system/node"

// Discover parses NUMA / CPU topology from Linux sysfs.
func Discover() (*Topology, error) {
	presentPath := filepath.Join(sysfsCPU, "present")
	raw, err := os.ReadFile(presentPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", presentPath, err)
	}
	cpuIDs, err := ParseCPUList(strings.TrimSpace(string(raw)))
	if err != nil || len(cpuIDs) == 0 {
		return nil, fmt.Errorf("no cpus in sysfs present list")
	}

	numaCPUs := map[int][]int{}
	nodeDirs, err := os.ReadDir(sysfsNode)
	if err != nil {
		nodeDirs = nil
	}
	for _, ent := range nodeDirs {
		if !strings.HasPrefix(ent.Name(), "node") || !ent.IsDir() {
			continue
		}
		idStr := strings.TrimPrefix(ent.Name(), "node")
		nid, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		listPath := filepath.Join(sysfsNode, ent.Name(), "cpulist")
		data, err := os.ReadFile(listPath)
		if err != nil {
			continue
		}
		list, err := ParseCPUList(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		numaCPUs[nid] = list
	}

	cpuNUMAMap := map[int]int{}
	for nid, cpus := range numaCPUs {
		for _, id := range cpus {
			cpuNUMAMap[id] = nid
		}
	}

	var cpus []CPU
	for _, id := range cpuIDs {
		cpuDir := filepath.Join(sysfsCPU, fmt.Sprintf("cpu%d", id))
		topo := filepath.Join(cpuDir, "topology")

		pkg := readIntFile(filepath.Join(topo, "physical_package_id"), 0)
		core := readIntFile(filepath.Join(topo, "core_id"), id)

		sibRaw, _ := os.ReadFile(filepath.Join(topo, "thread_siblings_list"))
		siblings, _ := ParseCPUList(strings.TrimSpace(string(sibRaw)))
		if len(siblings) == 0 {
			siblings = []int{id}
		}

		numa := -1
		if n, ok := cpuNUMAMap[id]; ok {
			numa = n
		}

		cpus = append(cpus, CPU{
			ID:        id,
			PackageID: pkg,
			CoreID:    core,
			NUMANode:  numa,
			Siblings:  siblings,
		})
	}

	var nodes []NUMANode
	for _, ent := range nodeDirs {
		if !strings.HasPrefix(ent.Name(), "node") || !ent.IsDir() {
			continue
		}
		idStr := strings.TrimPrefix(ent.Name(), "node")
		nid, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		listPath := filepath.Join(sysfsNode, ent.Name(), "cpulist")
		data, err := os.ReadFile(listPath)
		if err != nil {
			continue
		}
		cpulist, err := ParseCPUList(strings.TrimSpace(string(data)))
		if err != nil {
			continue
		}
		var dist []int
		distRaw, err := os.ReadFile(filepath.Join(sysfsNode, ent.Name(), "distance"))
		if err == nil {
			for _, f := range strings.Fields(strings.TrimSpace(string(distRaw))) {
				v, _ := strconv.Atoi(f)
				dist = append(dist, v)
			}
		}
		nodes = append(nodes, NUMANode{ID: nid, CPUs: cpulist, Distance: dist})
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	return &Topology{CPUs: cpus, NUMANodes: nodes}, nil
}

func readIntFile(path string, def int) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return def
	}
	return v
}
