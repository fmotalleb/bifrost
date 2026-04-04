package cmd

import (
	"bytes"
	"encoding/json"
	"net"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
)

func TestFilterIfaces(t *testing.T) {
	ifaces := []net.Interface{
		{Name: "down0", Flags: 0},
		{Name: "up0", Flags: net.FlagUp},
		{Name: "up1", Flags: net.FlagUp | net.FlagRunning},
	}

	t.Run("default_only_up", func(t *testing.T) {
		filtered := filterIfaces(ifaces, false)
		got := []string{filtered[0].Name, filtered[1].Name}
		want := []string{"up0", "up1"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected interfaces: got %v want %v", got, want)
		}
	})

	t.Run("all_keeps_everything", func(t *testing.T) {
		filtered := filterIfaces(ifaces, true)
		if !reflect.DeepEqual(filtered, ifaces) {
			t.Fatalf("unexpected interfaces when --all is set: got %#v want %#v", filtered, ifaces)
		}
	})
}

func TestIfaceFlagLabelsAndFormatFlags(t *testing.T) {
	flags := net.FlagUp | net.FlagBroadcast | net.FlagMulticast
	labels := ifaceFlagLabels(flags)
	wantLabels := []string{"up", "broadcast", "multicast"}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Fatalf("unexpected labels: got %v want %v", labels, wantLabels)
	}

	if got := formatFlags(0); got != "-" {
		t.Fatalf("formatFlags(0) = %q, want \"-\"", got)
	}
}

func TestPrintIfacesJSONNames(t *testing.T) {
	var out bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&out)

	ifaces := []ifaceInfo{{Name: "eth0"}, {Name: "wlan0"}}
	if err := printIfacesJSON(command, ifaces, false); err != nil {
		t.Fatalf("printIfacesJSON returned error: %v", err)
	}

	var names []string
	if err := json.Unmarshal(out.Bytes(), &names); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	want := []string{"eth0", "wlan0"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("unexpected json names: got %v want %v", names, want)
	}
}
