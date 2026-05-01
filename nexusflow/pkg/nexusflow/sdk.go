// Package nexusflow is the stable Go SDK entrypoint for NexusFlow primitives.
package nexusflow

import (
	"context"

	"github.com/kube-metrics/nexusflow/pkg/dag"
	"github.com/kube-metrics/nexusflow/pkg/hwloc"
	"github.com/kube-metrics/nexusflow/pkg/topology"
)

// DiscoverTopology reads sysfs topology (Linux).
func DiscoverTopology() (*topology.Topology, error) {
	return topology.Discover()
}

// DiscoverTopologyHWLoc runs hwloc-ls/lstopo XML export when installed.
func DiscoverTopologyHWLoc() (*topology.Topology, error) {
	return hwloc.DiscoverCLI()
}

// TopologyJSON mirrors CLI --json export (indent).
func TopologyJSON(t *topology.Topology, source string) ([]byte, error) {
	return topology.MarshalTopologyJSONIndent(t, "", "  ", source)
}

// TopologyFromSnapshotJSON parses CLI/SDK JSON layout back into Topology.
func TopologyFromSnapshotJSON(data []byte) (*topology.Topology, error) {
	return topology.UnmarshalTopologyJSON(data)
}

// TopologyFromHWLocXML parses hwloc XML bytes (offline SDK use).
func TopologyFromHWLocXML(xml []byte) (*topology.Topology, error) {
	return hwloc.TopologyFromXML(xml)
}

// LoadDAGYAML loads pipeline YAML from disk.
func LoadDAGYAML(path string) (*dag.Pipeline, error) {
	return dag.LoadFile(path)
}

// RunDAG executes a pipeline using sysfs topology for pinning decisions.
func RunDAG(ctx context.Context, topo *topology.Topology, p *dag.Pipeline) error {
	r := dag.Runner{Topo: topo}
	return r.Run(ctx, p)
}
