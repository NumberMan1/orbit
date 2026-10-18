package main

import "testing"

func TestRuntimeExample(t *testing.T) {
	if err := run(t.Context()); err != nil {
		t.Fatal(err)
	}
}
