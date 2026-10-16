package service

import "testing"

func TestNodesDoNotReplaceOrAlias(t *testing.T) {
	var nodes Nodes[int]
	if err := nodes.Add("one", 1); err != nil {
		t.Fatal(err)
	}
	if err := nodes.Add("one", 2); err == nil {
		t.Fatal("replaced node")
	}
	copy := nodes.Snapshot()
	delete(copy, "one")
	if value, ok := nodes.Get("one"); !ok || value != 1 {
		t.Fatal(value, ok)
	}
	if value, ok := nodes.Remove("one"); !ok || value != 1 {
		t.Fatal(value, ok)
	}
}
