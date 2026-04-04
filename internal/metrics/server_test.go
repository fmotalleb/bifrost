package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/fmotalleb/bifrost/internal/proxy"
)

func TestNormalizeIface(t *testing.T) {
	if got := normalizeIface("  ETH0 "); got != "eth0" {
		t.Fatalf("normalizeIface returned %q, want eth0", got)
	}
	if got := normalizeIface(" \t "); got != unassignedIface {
		t.Fatalf("normalizeIface blank returned %q, want %q", got, unassignedIface)
	}
}

func TestRecorderSnapshotTotals(t *testing.T) {
	rec := newRecorder([]string{"eth0"}, prometheus.NewRegistry())

	rec.AddTransfer("ETH0", proxy.DirectionTX, 100)
	rec.AddTransfer("eth0", proxy.DirectionRX, 25)
	rec.AddActiveConnections("eth0", 1)
	rec.AddTransfer("eth0", "invalid", 50)
	rec.AddTransfer("eth0", proxy.DirectionTX, -1)
	rec.ObserveConnection("eth0", true, 100, 25)
	rec.ObserveConnection("", false, 0, 0)

	snap := rec.Snapshot()
	if snap.TotalSuccess != 1 {
		t.Fatalf("TotalSuccess = %d, want 1", snap.TotalSuccess)
	}
	if snap.TotalFailed != 1 {
		t.Fatalf("TotalFailed = %d, want 1", snap.TotalFailed)
	}
	if snap.TotalTXBytes != 100 {
		t.Fatalf("TotalTXBytes = %d, want 100", snap.TotalTXBytes)
	}
	if snap.TotalRXBytes != 25 {
		t.Fatalf("TotalRXBytes = %d, want 25", snap.TotalRXBytes)
	}
	if snap.TotalActiveConnections != 1 {
		t.Fatalf("TotalActiveConnections = %d, want 1", snap.TotalActiveConnections)
	}

	eth0 := findIfaceSnapshot(t, snap, "eth0")
	if eth0.Success != 1 || eth0.Failed != 0 || eth0.TXBytes != 100 || eth0.RXBytes != 25 || eth0.ActiveConnections != 1 {
		t.Fatalf("unexpected eth0 snapshot: %+v", eth0)
	}

	unassigned := findIfaceSnapshot(t, snap, unassignedIface)
	if unassigned.Failed != 1 {
		t.Fatalf("unexpected unassigned snapshot: %+v", unassigned)
	}
}

func TestRecorderActiveConnectionsClamp(t *testing.T) {
	rec := newRecorder([]string{"eth0"}, prometheus.NewRegistry())

	rec.AddActiveConnections("eth0", 2)
	rec.AddActiveConnections("eth0", -5)
	rec.AddActiveConnections("eth1", -1)

	snap := rec.Snapshot()
	if snap.TotalActiveConnections != 0 {
		t.Fatalf("TotalActiveConnections = %d, want 0", snap.TotalActiveConnections)
	}

	eth0 := findIfaceSnapshot(t, snap, "eth0")
	if eth0.ActiveConnections != 0 {
		t.Fatalf("eth0 active = %d, want 0", eth0.ActiveConnections)
	}

	eth1 := findIfaceSnapshot(t, snap, "eth1")
	if eth1.ActiveConnections != 0 {
		t.Fatalf("eth1 active = %d, want 0", eth1.ActiveConnections)
	}
}

func findIfaceSnapshot(t *testing.T, snapshot Snapshot, name string) IfaceSnapshot {
	t.Helper()
	for _, iface := range snapshot.Ifaces {
		if iface.Name == name {
			return iface
		}
	}
	t.Fatalf("interface snapshot %q not found", name)
	return IfaceSnapshot{}
}
