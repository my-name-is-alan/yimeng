package library

import "strings"

// normGenre collapses common aliases (rock/Rock/搖滾 → rock). Empty stays empty.
func normGenre(g string) string {
	g = strings.TrimSpace(g)
	if g == "" {
		return ""
	}
	key := strings.ToLower(g)
	aliases := map[string]string{
		"r&b": "r&b", "rnb": "r&b", "hiphop": "hip-hop", "hip hop": "hip-hop",
		"hip-hop": "hip-hop", "電子": "electronic", "电子": "electronic",
		"流行": "pop", "摇滚": "rock", "搖滾": "rock", "古典": "classical",
		"ost": "soundtrack", "sound track": "soundtrack",
	}
	if v, ok := aliases[key]; ok {
		return v
	}
	return key
}
