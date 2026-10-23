package loudness

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func TestParse(t *testing.T) {
	out := "[Parsed_ebur128_0] Summary:\n\n  Integrated loudness:\n    I:         -14.2 LUFS\n    Threshold: -24.5 LUFS\n\n  True peak:\n    Peak:       -1.0 dBFS\n"
	l, p, err := parse(out)
	if err != nil || l != -14.2 || p < 0.89 || p > 0.9 {
		t.Fatal(l, p, err)
	}
	if _, _, err := parse("    I:         -inf LUFS\n"); err == nil {
		t.Fatal("silence")
	}
}

func TestPassWithFFmpeg(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	dir := t.TempDir()
	ctx := context.Background()
	d, _ := db.Open(filepath.Join(dir, "x.db"))
	defer d.Close()
	db.Migrate(ctx, d, migrations.FS)
	for i, vol := range []string{"0.5", "0.1"} {
		p := filepath.Join(dir, "t"+vol+".wav")
		if out, err := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i",
			"sine=frequency=1000:duration=2", "-af", "volume="+vol, "file:"+p).CombinedOutput(); err != nil {
			t.Fatal(string(out))
		}
		id := string(rune('a' + i))
		d.Exec(`INSERT INTO source_tracks(id, source, source_track_id, path, duration_ms, created_at, updated_at) VALUES (?,?,?,?,2000,1,1)`, id, "local", p, p)
		d.Exec(`INSERT INTO tracks(id, title, album_id, created_at, updated_at) VALUES (?,?,NULL,1,1)`, "t"+id, id)
		d.Exec(`INSERT INTO albums(id, title, created_at, updated_at) VALUES ('al','A',1,1)`)
		d.Exec(`UPDATE tracks SET album_id='al' WHERE id=?`, "t"+id)
		d.Exec(`INSERT INTO track_links(track_id, source_track_id) VALUES (?,?)`, "t"+id, id)
	}
	d.Exec(`INSERT INTO source_tracks(id, source, source_track_id, path, created_at, updated_at) VALUES ('z','local','/nope','/nope.wav',1,1)`)
	a := New(d, ff, nil)
	n, err := a.Pass(ctx)
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	var g1, g2, ag1, ag2 float64
	d.QueryRow(`SELECT rg_track_gain, rg_album_gain FROM source_tracks WHERE id='a'`).Scan(&g1, &ag1)
	d.QueryRow(`SELECT rg_track_gain, rg_album_gain FROM source_tracks WHERE id='b'`).Scan(&g2, &ag2)
	if g2-g1 < 13 || g2-g1 > 15 || ag1 != ag2 || ag1 <= g1 || ag1 >= g2 {
		t.Fatalf("track %v %v album %v %v", g1, g2, ag1, ag2)
	}
	var e string
	d.QueryRow(`SELECT loudness_error FROM source_tracks WHERE id='z'`).Scan(&e)
	if e == "" {
		t.Fatal("failure not recorded")
	}
	if n, _ := a.Pass(ctx); n != 0 {
		t.Fatal("re-analyzed")
	}
}
