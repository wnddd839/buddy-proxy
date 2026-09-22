package models

import "testing"

func TestLowestMultiplierID(t *testing.T) {
	t.Parallel()

	ptr := func(v float64) *float64 { return &v }

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if _, ok := LowestMultiplierID(nil); ok {
			t.Fatal("expected false for empty list")
		}
	})

	t.Run("free wins", func(t *testing.T) {
		t.Parallel()
		id, ok := LowestMultiplierID([]Model{
			{ID: "paid", CreditMultiplier: ptr(0.29)},
			{ID: "free", CreditMultiplier: ptr(0)},
			{ID: "mid", CreditMultiplier: ptr(0.1)},
		})
		if !ok || id != "free" {
			t.Fatalf("got %q ok=%v", id, ok)
		}
	})

	t.Run("missing multipliers fall back to first", func(t *testing.T) {
		t.Parallel()
		id, ok := LowestMultiplierID([]Model{
			{ID: "a"},
			{ID: "b"},
		})
		if !ok || id != "a" {
			t.Fatalf("got %q ok=%v", id, ok)
		}
	})

	t.Run("known beats unknown", func(t *testing.T) {
		t.Parallel()
		id, ok := LowestMultiplierID([]Model{
			{ID: "unknown"},
			{ID: "cheap", CreditMultiplier: ptr(0.5)},
			{ID: "also-unknown"},
		})
		if !ok || id != "cheap" {
			t.Fatalf("got %q ok=%v", id, ok)
		}
	})

	t.Run("stable tie break keeps earlier", func(t *testing.T) {
		t.Parallel()
		id, ok := LowestMultiplierID([]Model{
			{ID: "first", CreditMultiplier: ptr(0.1)},
			{ID: "second", CreditMultiplier: ptr(0.1)},
		})
		if !ok || id != "first" {
			t.Fatalf("got %q ok=%v", id, ok)
		}
	})

	t.Run("skips blank ids", func(t *testing.T) {
		t.Parallel()
		id, ok := LowestMultiplierID([]Model{
			{ID: "  ", CreditMultiplier: ptr(0)},
			{ID: "real", CreditMultiplier: ptr(1)},
		})
		if !ok || id != "real" {
			t.Fatalf("got %q ok=%v", id, ok)
		}
	})
}
