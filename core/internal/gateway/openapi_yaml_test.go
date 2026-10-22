package gateway

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// The spec must be valid YAML (designer tooling parses it strictly).
func TestOpenAPIParses(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := yaml.Unmarshal(b, &v); err != nil {
		t.Fatalf("openapi.yaml does not parse: %v", err)
	}
	if v["openapi"] == nil || v["paths"] == nil {
		t.Fatal("openapi.yaml missing openapi/paths")
	}
}
