package proxy

import (
	"testing"

	"github.com/fmotalleb/bifrost/config"
)

func TestNewSelectorValidation(t *testing.T) {
	if _, err := NewSelector(nil); err == nil {
		t.Fatal("expected error for nil/empty selector config")
	}

	_, err := NewSelector(map[string]config.Iface{
		"eth0": {Weight: 0},
	})
	if err == nil {
		t.Fatal("expected error for non-positive weight")
	}
}

func TestSelectorPickWeightedLeastActive(t *testing.T) {
	sel, err := NewSelector(map[string]config.Iface{
		"eth0": {Weight: 1},
		"eth1": {Weight: 2},
	})
	if err != nil {
		t.Fatalf("NewSelector failed: %v", err)
	}

	first, _ := sel.Pick()
	second, _ := sel.Pick()
	third, _ := sel.Pick()

	if first != "eth0" || second != "eth1" || third != "eth1" {
		t.Fatalf("unexpected pick order: %q, %q, %q", first, second, third)
	}

	sel.Release("eth1")
	next, _ := sel.Pick()
	if next != "eth1" {
		t.Fatalf("expected eth1 after release, got %q", next)
	}
}

func TestSelectorTieRoundRobin(t *testing.T) {
	sel, err := NewSelector(map[string]config.Iface{
		"a": {Weight: 1},
		"b": {Weight: 1},
	})
	if err != nil {
		t.Fatalf("NewSelector failed: %v", err)
	}

	p1, _ := sel.Pick()
	p2, _ := sel.Pick()
	p3, _ := sel.Pick()
	p4, _ := sel.Pick()

	if p1 != "a" || p2 != "b" || p3 != "b" || p4 != "a" {
		t.Fatalf("unexpected tie order: %q, %q, %q, %q", p1, p2, p3, p4)
	}
}

func TestCompareLoadRatio(t *testing.T) {
	if got := compareLoadRatio(1, 2, 1, 1); got >= 0 {
		t.Fatalf("expected first ratio to be lower, got %d", got)
	}
	if got := compareLoadRatio(3, 1, 1, 1); got <= 0 {
		t.Fatalf("expected first ratio to be higher, got %d", got)
	}
	if got := compareLoadRatio(2, 2, 1, 1); got != 0 {
		t.Fatalf("expected equal ratios, got %d", got)
	}
}
