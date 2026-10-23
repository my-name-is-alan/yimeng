package logging

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	l, err := New(&b, "json", "warn")
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hidden")
	l.Warn("shown", "k", 1)
	var m map[string]any
	if err := json.Unmarshal(b.Bytes(), &m); err != nil || m["msg"] != "shown" {
		t.Fatalf("%s", b.String())
	}
	if _, err := New(&b, "xml", ""); err == nil {
		t.Fatal("bad format")
	}
}
