package protogen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedBindingsCompileWithShadowingNames(t *testing.T) {
	for _, tc := range []struct{ name, manifest, extra string }{
		{"import", `{"package":"demo","imports":{"r":"github.com/NumberMan1/orbit/config"},"messages":[{"id":1,"type":"r.Config","codec":"json","handler":"Handle"}]}`, ""},
		{"local", `{"package":"demo","messages":[{"id":1,"type":"r","codec":"json"}]}`, "\ntype r struct{}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := Generate(strings.NewReader(tc.manifest))
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "bindings.go")
			if err := os.WriteFile(file, append(data, []byte(tc.extra)...), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), "go", "test", file)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("generated code did not compile: %v\n%s", err, output)
			}
		})
	}
	for _, name := range []string{"RegisterMessages", "RegisterHandlers", "Handlers", "codec", "context", "nil", "error"} {
		manifest := `{"package":"demo","messages":[{"id":1,"type":"` + name + `","codec":"json","handler":"Handle"}]}`
		if _, err := Generate(strings.NewReader(manifest)); err == nil {
			t.Fatalf("accepted conflicting type %s", name)
		}
	}
}
