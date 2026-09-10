package strutil

import (
	"encoding/json"
	"testing"
)

func TestFirst(t *testing.T) {
	if got := First(" ", "", "<nil>", " a ", "b"); got != "a" {
		t.Fatalf("First=%q", got)
	}
	if got := First("", " "); got != "" {
		t.Fatalf("empty First=%q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("  abcdef  ", 3); got != "abc" {
		t.Fatalf("Truncate=%q", got)
	}
	if got := Truncate("ab", 5); got != "ab" {
		t.Fatalf("short Truncate=%q", got)
	}
}

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("abcdefghijkl", 3); got != "abc...jkl" {
		t.Fatalf("MaskSecret=%q", got)
	}
	if got := MaskSecret("ab", 3); got != "ab..." {
		t.Fatalf("short MaskSecret=%q", got)
	}
}

func TestPositiveInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{1_000_000, 1_000_000},
		{int64(50_000), 50_000},
		{float64(1_000_000), 1_000_000},
		{json.Number("64000"), 64_000},
		{"48000", 48_000},
		{0, 0},
		{-1, 0},
		{1.5, 0},
		{"", 0},
		{nil, 0},
	}
	for _, tc := range cases {
		if got := PositiveInt(tc.in); got != tc.want {
			t.Fatalf("PositiveInt(%v)=%d want %d", tc.in, got, tc.want)
		}
	}
	if got := FirstPositiveInt(0, float64(0), 1_000_000, 50_000); got != 1_000_000 {
		t.Fatalf("FirstPositiveInt=%d", got)
	}
}

func TestRandomHex(t *testing.T) {
	a := RandomHex(8)
	b := RandomHex(8)
	if len(a) != 16 || len(b) != 16 {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	if a == b {
		t.Fatal("expected different random hex")
	}
}
