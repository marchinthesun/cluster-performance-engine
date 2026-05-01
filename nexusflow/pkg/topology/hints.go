package topology

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// SocketSummary describes one CPU package / socket for orchestration hints.
type SocketSummary struct {
	PackageID     int    `json:"package_id"`
	LogicalCPUs   int    `json:"logical_cpus"`
	PhysicalCores int    `json:"physical_cores"`
	CPUList       string `json:"cpu_list"`
}

// NUMASummary describes one NUMA domain for pinning scripts.
type NUMASummary struct {
	NodeID      int    `json:"node_id"`
	LogicalCPUs int    `json:"logical_cpus"`
	CPUList     string `json:"cpu_list"`
}

// ClusterHints aggregates topology-derived guidance for Slurm, MPI wrappers, and CI build parallelism.
type ClusterHints struct {
	SourceUsed string `json:"source_used,omitempty"`

	LogicalCPUs        int `json:"logical_cpus"`
	PhysicalCoresTotal int `json:"physical_cores_total"`
	SocketCount        int `json:"socket_count"`
	NUMANodeCount      int `json:"numa_node_count"`

	Sockets   []SocketSummary `json:"sockets"`
	NUMANodes []NUMASummary   `json:"numa_nodes"`

	MaxLogicalPerSocket   int `json:"max_logical_cpus_per_socket"`
	MaxLogicalPerNUMANode int `json:"max_logical_cpus_per_numa_node"`
	MinLogicalPerNUMANode int `json:"min_logical_cpus_per_numa_node"`

	// SuggestedCPUsPerTaskSocketLocal is a default when dedicating one Slurm/MPI rank per socket.
	SuggestedCPUsPerTaskSocketLocal int `json:"suggested_cpus_per_task_socket_local"`

	FirstSocketCPUList string `json:"first_socket_cpu_list"`

	MakefileJWholeNodeLogical int   `json:"make_j_whole_node_logical"`
	MakefileJPerSocketCore    []int `json:"make_j_per_socket_physical_cores"`
}

type coreKey struct {
	pkg, core int
}

// BuildClusterHints derives orchestration hints from a topology snapshot.
func BuildClusterHints(t *Topology, sourceUsed string) *ClusterHints {
	if t == nil {
		return &ClusterHints{SourceUsed: sourceUsed}
	}

	h := &ClusterHints{
		SourceUsed:                sourceUsed,
		LogicalCPUs:               len(t.CPUs),
		SocketCount:               len(t.PackageIDs()),
		NUMANodeCount:             len(t.NUMANodes),
		MakefileJWholeNodeLogical: len(t.CPUs),
	}

	coreSeen := map[coreKey]struct{}{}
	pkgCPUs := map[int][]int{}
	for _, c := range t.CPUs {
		pkgCPUs[c.PackageID] = append(pkgCPUs[c.PackageID], c.ID)
		coreSeen[coreKey{c.PackageID, c.CoreID}] = struct{}{}
	}
	h.PhysicalCoresTotal = len(coreSeen)

	pkgPhy := map[int]int{}
	for ck := range coreSeen {
		pkgPhy[ck.pkg]++
	}

	pkgIDs := t.PackageIDs()
	for _, pkg := range pkgIDs {
		cpus := append([]int(nil), pkgCPUs[pkg]...)
		sort.Ints(cpus)
		logical := len(cpus)
		phy := pkgPhy[pkg]
		h.Sockets = append(h.Sockets, SocketSummary{
			PackageID:     pkg,
			LogicalCPUs:   logical,
			PhysicalCores: phy,
			CPUList:       FormatCPUList(cpus),
		})
		if logical > h.MaxLogicalPerSocket {
			h.MaxLogicalPerSocket = logical
		}
	}

	h.MakefileJPerSocketCore = make([]int, len(h.Sockets))
	for i := range h.Sockets {
		h.MakefileJPerSocketCore[i] = h.Sockets[i].PhysicalCores
	}

	minNuma := 0
	maxNuma := 0
	firstNuma := true
	for _, n := range t.NUMANodes {
		cp := append([]int(nil), n.CPUs...)
		sort.Ints(cp)
		k := len(cp)
		h.NUMANodes = append(h.NUMANodes, NUMASummary{
			NodeID:      n.ID,
			LogicalCPUs: k,
			CPUList:     FormatCPUList(cp),
		})
		if firstNuma || k > maxNuma {
			maxNuma = k
		}
		if firstNuma || k < minNuma {
			minNuma = k
		}
		firstNuma = false
	}
	h.MaxLogicalPerNUMANode = maxNuma
	h.MinLogicalPerNUMANode = minNuma
	if len(h.NUMANodes) == 0 {
		h.MaxLogicalPerNUMANode = h.LogicalCPUs
		h.MinLogicalPerNUMANode = h.LogicalCPUs
	}

	h.SuggestedCPUsPerTaskSocketLocal = h.MaxLogicalPerSocket
	if h.SuggestedCPUsPerTaskSocketLocal <= 0 {
		h.SuggestedCPUsPerTaskSocketLocal = h.LogicalCPUs
	}

	if len(h.Sockets) > 0 {
		h.FirstSocketCPUList = h.Sockets[0].CPUList
	}

	return h
}

// FormatHintsJSON renders ClusterHints as indented JSON.
func FormatHintsJSON(h *ClusterHints) ([]byte, error) {
	if h == nil {
		h = &ClusterHints{}
	}
	return json.MarshalIndent(h, "", "  ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, "'") {
		return "'" + s + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// WriteHintsShell writes POSIX export lines consumed by Slurm job prologs or bash wrappers.
func WriteHintsShell(w io.Writer, h *ClusterHints) error {
	if h == nil {
		h = &ClusterHints{}
	}
	var sb strings.Builder
	sb.WriteString("# NexusFlow topology hints — eval: eval \"$(nexusflow topology hints --format shell)\"\n")
	fmt.Fprintf(&sb, "export NEXUSFLOW_TOPOLOGY_SOURCE=%s\n", shellQuote(h.SourceUsed))
	fmt.Fprintf(&sb, "export NEXUSFLOW_LOGICAL_CPUS=%d\n", h.LogicalCPUs)
	fmt.Fprintf(&sb, "export NEXUSFLOW_PHYSICAL_CORES=%d\n", h.PhysicalCoresTotal)
	fmt.Fprintf(&sb, "export NEXUSFLOW_SOCKET_COUNT=%d\n", h.SocketCount)
	fmt.Fprintf(&sb, "export NEXUSFLOW_NUMA_NODE_COUNT=%d\n", h.NUMANodeCount)
	fmt.Fprintf(&sb, "export NEXUSFLOW_MAX_LOGICAL_PER_SOCKET=%d\n", h.MaxLogicalPerSocket)
	fmt.Fprintf(&sb, "export NEXUSFLOW_MAX_LOGICAL_PER_NUMA=%d\n", h.MaxLogicalPerNUMANode)
	fmt.Fprintf(&sb, "export NEXUSFLOW_MIN_LOGICAL_PER_NUMA=%d\n", h.MinLogicalPerNUMANode)
	fmt.Fprintf(&sb, "export NEXUSFLOW_SUGGEST_CPUS_PER_TASK=%d\n", h.SuggestedCPUsPerTaskSocketLocal)
	fmt.Fprintf(&sb, "export NEXUSFLOW_MAKE_J_LOGICAL=%d\n", h.MakefileJWholeNodeLogical)
	fmt.Fprintf(&sb, "export NEXUSFLOW_FIRST_SOCKET_CPUS=%s\n", shellQuote(h.FirstSocketCPUList))

	srunExtra := fmt.Sprintf(
		"--cpu-bind=sockets --cpus-per-task=%d",
		h.SuggestedCPUsPerTaskSocketLocal,
	)
	fmt.Fprintf(&sb, "export NEXUSFLOW_SRUN_EXTRA=%s\n", shellQuote(srunExtra))

	mpirunArgs := "--bind-to socket --map-by socket"
	fmt.Fprintf(&sb, "export NEXUSFLOW_MPIRUN_HINT=%s\n", shellQuote(mpirunArgs))

	_, err := io.WriteString(w, sb.String())
	return err
}

// FormatHintsMatrix returns a plain-text table for CI logs (socket-local make -j and NUMA cpulists).
func FormatHintsMatrix(h *ClusterHints) string {
	if h == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# NexusFlow node matrix — paste into CI logs / runner docs\n")
	fmt.Fprintf(&b, "scope\tparallelism\tnote\n")
	fmt.Fprintf(&b, "whole-node\t-j%d\tlogical CPUs (hyperthreads included)\n", h.MakefileJWholeNodeLogical)
	for i := range h.Sockets {
		s := h.Sockets[i]
		fmt.Fprintf(&b, "socket-%d\t-j%d\tpackage=%d logical=%d physical_cores=%d cpulist=%s\n",
			i, s.PhysicalCores, s.PackageID, s.LogicalCPUs, s.PhysicalCores, s.CPUList)
	}
	if len(h.NUMANodes) > 0 {
		for _, n := range h.NUMANodes {
			fmt.Fprintf(&b, "numa-%d\tcpus=%d\tCPU_LIST=%s\n", n.NodeID, n.LogicalCPUs, n.CPUList)
		}
	}
	return b.String()
}

// WriteHintsOpenMPI writes Open MPI MCA-style hints (best-effort across versions).
func WriteHintsOpenMPI(w io.Writer, h *ClusterHints) error {
	if h == nil {
		h = &ClusterHints{}
	}
	var sb strings.Builder
	sb.WriteString("# Open MPI — verify with `ompi_info`; mpirun flags are often more portable.\n")
	fmt.Fprintf(&sb, "export NEXUSFLOW_MPIRUN_HINT=%s\n", shellQuote("--bind-to socket --map-by socket"))
	fmt.Fprintf(&sb, "export OMPI_MCA_hwloc_base_binding_policy=%s\n", shellQuote("socket"))
	fmt.Fprintf(&sb, "export OMPI_MCA_rmaps_base_mapping_policy=%s\n", shellQuote("socket"))
	fmt.Fprintf(&sb, "export NEXUSFLOW_SUGGEST_CPUS_PER_TASK=%d\n", h.SuggestedCPUsPerTaskSocketLocal)
	_, err := io.WriteString(w, sb.String())
	return err
}

// WriteHintsSlurm writes Slurm-oriented exports (same vars as shell; leading comment only).
func WriteHintsSlurm(w io.Writer, h *ClusterHints) error {
	if _, err := io.WriteString(w, "# Slurm — combine exports with SLURM_* set by sbatch/srun on the compute node.\n"); err != nil {
		return err
	}
	return WriteHintsShell(w, h)
}