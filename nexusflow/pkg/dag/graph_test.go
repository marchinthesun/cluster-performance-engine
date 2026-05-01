package dag

import (
	"strings"
	"testing"
)

func TestTopoOrder_linear(t *testing.T) {
	nodes := []Node{
		{ID: "a", Cmd: strings.Fields("echo a")},
		{ID: "b", Cmd: strings.Fields("echo b"), DependsOn: []string{"a"}},
	}
	o, err := TopoOrder(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(o) != 2 || o[0].ID != "a" || o[1].ID != "b" {
		t.Fatalf("got %+v", o)
	}
}

func TestTopoOrder_cycle(t *testing.T) {
	nodes := []Node{
		{ID: "a", Cmd: strings.Fields("echo a"), DependsOn: []string{"b"}},
		{ID: "b", Cmd: strings.Fields("echo b"), DependsOn: []string{"a"}},
	}
	_, err := TopoOrder(nodes)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}
