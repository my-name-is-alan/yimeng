package gateway

import (
	"os"
	"strings"
	"testing"
)

// Every route registered in NewRouter must appear in api/openapi.yaml.
func TestOpenAPICoversRoutes(t *testing.T) {
	spec, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := os.ReadFile("server.go")
	for _, line := range strings.Split(string(src), "\n") {
		i := strings.Index(line, `mux.HandleFunc("`)
		if i < 0 {
			continue
		}
		if strings.Contains(line, `+a`) {
			continue // player actions checked below
		}
		pat := strings.SplitN(line[i+len(`mux.HandleFunc("`):], `"`, 2)[0]
		_, path, _ := strings.Cut(pat, " ")
		if !strings.Contains(string(spec), "\n  "+path+":") {
			t.Errorf("route %q missing from openapi.yaml", pat)
		}
	}
	for _, a := range []string{"play", "pause", "stop", "next", "prev", "seek", "repeat", "queue"} {
		if !strings.Contains(string(spec), "\n  /api/players/{id}/"+a+":") {
			t.Errorf("player action %s missing", a)
		}
	}
}
