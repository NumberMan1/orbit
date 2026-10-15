package codec

import (
	"strings"
	"testing"
)

func TestJSONV2RejectsAmbiguousInput(t *testing.T) {
	for _, input := range []string{
		`{"value":"first","value":"second"}`,
		`{"value":"first","\u0076alue":"second"}`,
		`{"metadata":{"zone":"a","zone":"b"}}`,
		"{\"value\":\"\xff\"}",
	} {
		t.Run(input, func(t *testing.T) {
			var dst map[string]any
			if err := (JSON{}).Unmarshal([]byte(input), &dst); err == nil {
				t.Fatalf("accepted ambiguous or invalid JSON: %q", input)
			}
		})
	}
	if _, err := (JSON{}).Marshal(map[string]string{"value": "\xff"}); err == nil {
		t.Fatal("invalid UTF-8 encoded silently")
	}
}

func TestJSONV2FieldMatchingAndEmptyCollections(t *testing.T) {
	var dst struct {
		Value string `json:"value"`
	}
	if err := (JSON{}).Unmarshal([]byte(`{"VALUE":"ignored","value":"exact"}`), &dst); err != nil || dst.Value != "exact" {
		t.Fatalf("case-sensitive fields: %+v, %v", dst, err)
	}
	if err := (JSON{}).Unmarshal([]byte(`{"Value":"ignored"}`), &dst); err != nil || dst.Value != "exact" {
		t.Fatalf("case variant unexpectedly matched: %+v, %v", dst, err)
	}
	value := struct {
		Items  []string          `json:"items"`
		Labels map[string]string `json:"labels"`
		Count  int               `json:"count,omitempty"`
		Omit   int               `json:"omit,omitzero"`
	}{}
	encoded, err := (JSON{}).Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []string{`"items":[]`, `"labels":{}`, `"count":0`} {
		if !strings.Contains(string(encoded), member) {
			t.Fatalf("missing v2 representation %s in %s", member, encoded)
		}
	}
	if strings.Contains(string(encoded), `"omit"`) {
		t.Fatalf("omitzero ignored: %s", encoded)
	}
}
