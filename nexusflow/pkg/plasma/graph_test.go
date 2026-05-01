package plasma

import (
	"testing"

	"github.com/kube-metrics/nexusflow/pkg/dag"
)

func TestAppendDAGBranch(t *testing.T) {
	base := []dag.Node{
		{ID: "a", Cmd: []string{"true"}, DependsOn: nil},
	}
	add := []dag.Node{
		{ID: "b", Cmd: []string{"true"}, DependsOn: []string{"a"}},
	}
	merged, err := AppendDAG(base, add)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 2 {
		t.Fatalf("len=%d", len(merged))
	}
}

func TestAppendDAGCycleRejected(t *testing.T) {
	base := []dag.Node{
		{ID: "a", Cmd: []string{"true"}, DependsOn: []string{"b"}},
		{ID: "b", Cmd: []string{"true"}, DependsOn: []string{"a"}},
	}
	_, err := AppendDAG(nil, base)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}
