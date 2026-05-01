package dag

import (
	"fmt"
	"strings"
)

// Document is the root YAML envelope.
type Document struct {
	Pipeline Pipeline `yaml:"pipeline"`
}

// Pipeline is a DAG of shell-ish commands with optional CPU pinning hints.
type Pipeline struct {
	Name  string `yaml:"name"`
	Nodes []Node `yaml:"nodes"`
}

// Node is one vertex in the DAG.
type Node struct {
	ID        string   `yaml:"id"`
	Cmd       []string `yaml:"cmd"`
	CPUs      int      `yaml:"cpus"`
	NUMANode  *int     `yaml:"numa_node,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

// Validate checks ids and dependency references.
func Validate(p *Pipeline) error {
	if p == nil {
		return fmt.Errorf("nil pipeline")
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("pipeline.name required")
	}
	ids := map[string]struct{}{}
	for _, n := range p.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			return fmt.Errorf("node without id")
		}
		if _, dup := ids[n.ID]; dup {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		ids[n.ID] = struct{}{}
		if len(n.Cmd) == 0 {
			return fmt.Errorf("node %q: empty cmd", n.ID)
		}
	}
	for _, n := range p.Nodes {
		for _, d := range n.DependsOn {
			if _, ok := ids[d]; !ok {
				return fmt.Errorf("node %q: unknown dependency %q", n.ID, d)
			}
		}
	}
	return nil
}
