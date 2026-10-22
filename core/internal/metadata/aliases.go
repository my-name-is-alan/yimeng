package metadata

import (
	"context"
	"strings"
	"sync"
	"time"
)

// AliasLookup returns a cached alias resolver backed by a provider's artist
// lookup (MusicBrainz): for an exact name/alias hit it yields the artist's
// primary name and all aliases (e.g. 周杰伦 → Jay Chou, Zhou Jielun).
func AliasLookup(p MetadataProvider) func(ctx context.Context, name string) []string {
	if p == nil {
		return nil
	}
	type entry struct {
		names []string
		at    time.Time
	}
	var mu sync.Mutex
	cache := map[string]entry{}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	return func(ctx context.Context, name string) []string {
		key := norm(name)
		mu.Lock()
		if e, ok := cache[key]; ok && time.Since(e.at) < 24*time.Hour {
			mu.Unlock()
			return e.names
		}
		mu.Unlock()
		cctx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		infos, err := p.LookupArtist(cctx, ArtistQuery{Name: name})
		var out []string
		for _, a := range infos {
			hit := norm(a.Name) == key
			for _, al := range a.Aliases {
				hit = hit || norm(al) == key
			}
			if !hit {
				continue
			}
			out = append(out, a.Name)
			out = append(out, a.Aliases...)
			break
		}
		if err == nil || len(out) > 0 {
			mu.Lock()
			cache[key] = entry{out, time.Now()}
			mu.Unlock()
		}
		return out
	}
}
