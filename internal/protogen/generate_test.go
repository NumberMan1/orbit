package protogen

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestDeterministicBindingsAndInvalidManifests(t *testing.T) {
	manifest := `{"package":"demo","messages":[{"id":2,"type":"Reply","codec":"json"},{"id":1,"type":"Ping","codec":"json","handler":"Ping"}]}`
	a, err := Generate(strings.NewReader(manifest))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(strings.NewReader(manifest))
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("not deterministic", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "bindings.go", a, parser.AllErrors); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(manifest, `"id":2`, `"id":1`, 1),
		strings.Replace(manifest, `"Reply"`, `"Ping"`, 1),
		strings.Replace(manifest, `"Reply"`, `"x); panic(1)"`, 1),
		strings.Replace(manifest, `"json"`, `"custom"`, 1),
		strings.Replace(manifest, `"package"`, `"typo"`, 1),
	} {
		if _, err := Generate(strings.NewReader(bad)); err == nil {
			t.Fatal("invalid manifest accepted", bad)
		}
	}
}
