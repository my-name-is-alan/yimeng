package events

import "testing"

func TestHub(t *testing.T) {
	h := NewHub()
	a, ca := h.Subscribe()
	b, cb := h.Subscribe()
	h.Publish("x", 1)
	if (<-a).Type != "x" || (<-b).Type != "x" {
		t.Fatal()
	}
	ca()
	ca()                       // idempotent
	for i := 0; i < 100; i++ { // overflow drops b
		h.Publish("y", i)
	}
	if h.Subscribers() != 0 {
		t.Fatal("slow subscriber not dropped")
	}
	cb()
}
