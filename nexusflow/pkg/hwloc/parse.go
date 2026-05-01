package hwloc

import (
	"encoding/xml"
	"fmt"
	"os/exec"
	"regexp"
	"sort"

	"github.com/kube-metrics/nexusflow/pkg/topology"
)

// hwloc XML object (minimal subset).
type xmlObj struct {
	Type    string   `xml:"type,attr"`
	OsIndex *int     `xml:"os_index,attr"`
	Object  []xmlObj `xml:"object"`
}

func deref(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

type puRec struct {
	pu    int
	pkg   int
	numa  int
	core  int
}

func walk(o xmlObj, pkg, numa, core int, nextCore *int, out *[]puRec) {
	pkgN, numaN, coreN := pkg, numa, core
	switch o.Type {
	case "Package":
		pkgN = deref(o.OsIndex, pkgN)
	case "NUMANode":
		numaN = deref(o.OsIndex, numaN)
	case "Core":
		coreN = deref(o.OsIndex, *nextCore)
		*nextCore = coreN + 1
	case "PU":
		pu := deref(o.OsIndex, -1)
		if pu >= 0 {
			*out = append(*out, puRec{pu: pu, pkg: pkgN, numa: numaN, core: coreN})
		}
	}
	for _, ch := range o.Object {
		walk(ch, pkgN, numaN, coreN, nextCore, out)
	}
}

var xmlnsStrip = regexp.MustCompile(`\s+xmlns="[^"]*"`)

// TopologyFromXML parses hwloc-exported XML (hwloc-ls / lstopo --of xml).
func TopologyFromXML(xmlData []byte) (*topology.Topology, error) {
	xmlData = xmlnsStrip.ReplaceAll(xmlData, nil)
	var root struct {
		XMLName xml.Name `xml:"topology"`
		Object  []xmlObj `xml:"object"`
	}
	if err := xml.Unmarshal(xmlData, &root); err != nil {
		return nil, fmt.Errorf("hwloc xml: %w", err)
	}
	var recs []puRec
	coreSeq := 0
	for _, o := range root.Object {
		walk(o, -1, -1, -1, &coreSeq, &recs)
	}
	if len(recs) == 0 {
		return nil, fmt.Errorf("hwloc xml: no PU objects found")
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].pu < recs[j].pu })

	numaMap := map[int][]int{}
	var cpus []topology.CPU
	for _, r := range recs {
		sib := []int{r.pu}
		coreID := r.core
		if coreID < 0 {
			coreID = r.pu
		}
		cpus = append(cpus, topology.CPU{
			ID:        r.pu,
			PackageID: max0(r.pkg),
			CoreID:    coreID,
			NUMANode:  r.numa,
			Siblings:  sib,
		})
		if r.numa >= 0 {
			numaMap[r.numa] = append(numaMap[r.numa], r.pu)
		}
	}
	var nodes []topology.NUMANode
	for nid := range numaMap {
		cp := append([]int(nil), numaMap[nid]...)
		sort.Ints(cp)
		nodes = append(nodes, topology.NUMANode{ID: nid, CPUs: cp})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	return &topology.Topology{CPUs: cpus, NUMANodes: nodes}, nil
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// DiscoverCLI runs hwloc-ls or lstopo-no-graphics and parses XML (requires binaries on PATH).
func DiscoverCLI() (*topology.Topology, error) {
	try := [][]string{
		{"hwloc-ls", "--no-io", "--of", "xml"},
		{"lstopo-no-graphics", "--no-io", "--of", "xml"},
		{"lstopo", "--no-io", "--of", "xml"},
	}
	var lastErr error
	for _, argv := range try {
		out, err := exec.Command(argv[0], argv[1:]...).Output()
		if err != nil {
			lastErr = err
			continue
		}
		return TopologyFromXML(out)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("hwloc: hwloc-ls/lstopo failed: %w", lastErr)
	}
	return nil, fmt.Errorf("hwloc: no exporter found")
}
