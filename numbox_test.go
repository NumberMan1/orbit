package orbit_test

import (
	"reflect"
	"testing"

	"github.com/NumberMan1/numbox/pkg/matcher"
)

func TestMatcherFailureLinksPreserveSuffixesAndOrdering(t *testing.T) {
	m := matcher.NewMatcher([]string{"abcd", "bc", "c", "cd", "abcd"})
	want := []matcher.Match{{Start: 1, End: 5}, {Start: 2, End: 4}, {Start: 3, End: 5}, {Start: 3, End: 4}}
	if got := m.Find("界abcd!"); !reflect.DeepEqual(got, want) {
		t.Fatalf("suffix matches or rune ordering changed: %v", got)
	}
	if got := m.Replace("界abcd!", '*'); got != "界****!" {
		t.Fatal(got)
	}
	if !m.Contains("界abc!") || m.Contains("界ab!") {
		t.Fatal("failure-link suffix detection changed")
	}
}
