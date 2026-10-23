package library

import "testing"

func TestNormGenre(t *testing.T) {
	if normGenre("Hip Hop") != "hip-hop" {
		t.Fatal(normGenre("Hip Hop"))
	}
	if normGenre("搖滾") != "rock" {
		t.Fatal(normGenre("搖滾"))
	}
	if normGenre("") != "" {
		t.Fatal("empty")
	}
}
