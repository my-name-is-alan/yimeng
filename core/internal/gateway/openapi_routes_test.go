package gateway

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestEveryRouteDocumented fails when a route registered in this package is
// missing from api/openapi.yaml. /api/v1/x and /api/x are the same route
// (apiV1Alias); path parameter names are ignored. A wildcard route such as
// /api/online/{origin}/{sub} counts as documented when the spec lists at
// least one concrete path it serves.
func TestEveryRouteDocumented(t *testing.T) {
	routeRe := regexp.MustCompile(`Handle(?:Func)?\(\s*"([A-Z]+) (/[^"]*)"`)
	files, _ := filepath.Glob("*.go")
	type route struct{ method, path string }
	var routes []route
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range routeRe.FindAllStringSubmatch(string(b), -1) {
			routes = append(routes, route{m[1], m[2]})
		}
	}
	if len(routes) < 100 {
		t.Fatalf("only %d routes found; extraction broken?", len(routes))
	}
	spec, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	norm := func(p string) []string {
		p = strings.TrimSuffix(p, "/")
		p = regexp.MustCompile(`^/api/v1(/|$)`).ReplaceAllString(p, "/api$1")
		return strings.Split(p, "/")
	}
	pathRe := regexp.MustCompile(`^  (/[^:]*):\s*$`)
	opRe := regexp.MustCompile(`^    (get|post|put|patch|delete|head):`)
	doc := map[string][][]string{} // METHOD -> documented paths (segments)
	var cur string
	for _, l := range strings.Split(string(spec), "\n") {
		if m := pathRe.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if m := opRe.FindStringSubmatch(l); m != nil && cur != "" {
			k := strings.ToUpper(m[1])
			doc[k] = append(doc[k], norm(cur))
		}
	}
	param := func(s string) bool { return strings.HasPrefix(s, "{") }
	matches := func(route, d []string) bool {
		if len(route) != len(d) {
			return false
		}
		for i := range route {
			if route[i] == d[i] || param(route[i]) {
				continue
			}
			return false
		}
		return true
	}
	// Not part of the JSON API: web console shell + redirects, Subsonic (/rest,
	// documented in docs/developer-api.md), health probe.
	skip := func(p string) bool {
		return p == "/{$}" || p == "/admin" || strings.HasPrefix(p, "/admin/") || strings.HasPrefix(p, "/rest") || p == "/healthz" || p == "/metrics"
	}
	var missing []string
	for _, r := range routes {
		if skip(r.path) {
			continue
		}
		if strings.HasSuffix(r.path, "/") && subtreeDocumented(doc[r.method], norm(r.path+"x")) {
			continue
		}
		rp := norm(strings.TrimSuffix(r.path, "{$}"))
		cands := doc[r.method]
		if r.method == "HEAD" {
			cands = append(cands, doc["GET"]...)
		}
		ok := false
		for _, d := range cands {
			if matches(rp, d) {
				ok = true
				break
			}
		}
		if !ok {
			missing = append(missing, r.method+" "+r.path)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d routes missing from api/openapi.yaml:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}

// subtreeDocumented: a "/prefix/" subtree route is documented when the spec has
// any path under that prefix (e.g. /api/players/{id}/ → /players/{id}/play).
func subtreeDocumented(docs [][]string, probe []string) bool {
	pre := probe[:len(probe)-1]
	for _, d := range docs {
		if len(d) <= len(pre) {
			continue
		}
		ok := true
		for i := range pre {
			if pre[i] != d[i] && !strings.HasPrefix(pre[i], "{") {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
