package logging

import (
	"io"
	"log/slog"
	"testing"
)

func TestRing(t *testing.T) {
	r := NewRing(3)
	log := slog.New(r.Wrap(slog.NewTextHandler(io.Discard, nil)))
	log.Info("a")
	log.With("token", "s3cret").Warn("b", "n", 1)
	log.Error("c")
	log.Info("d")
	all := r.Since(0, slog.LevelDebug, 0)
	if len(all) != 3 || all[0].Msg != "b" || all[2].Msg != "d" {
		t.Fatalf("%+v", all)
	}
	if all[0].Attrs["token"] != "[redacted]" || all[0].Attrs["n"] != "1" {
		t.Fatalf("%+v", all[0].Attrs)
	}
	if w := r.Since(0, slog.LevelWarn, 0); len(w) != 2 {
		t.Fatalf("%+v", w)
	}
	if s := r.Since(all[1].Seq, slog.LevelDebug, 0); len(s) != 1 || s[0].Msg != "d" {
		t.Fatalf("%+v", s)
	}
}
