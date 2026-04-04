// Package cmd defines bifrost CLI commands.
package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	detailedIfaces bool
	jsonIfaces     bool
	allIfaces      bool
)

const ifaceFlagsCapacity = 6

type ifaceInfo struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MTU       int      `json:"mtu"`
	MAC       string   `json:"mac"`
	Flags     []string `json:"flags"`
	Addresses []string `json:"addresses"`
}

// listIfacesCmd prints network interfaces available on the local host.
var listIfacesCmd = &cobra.Command{
	Use:   "list-ifaces",
	Short: "List available network interfaces",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ifaces, err := collectIfaceDetails(allIfaces)
		if err != nil {
			return err
		}

		if jsonIfaces {
			return printIfacesJSON(cmd, ifaces, detailedIfaces)
		}

		return printIfacesPlain(cmd, ifaces, detailedIfaces)
	},
}

func collectIfaceDetails(includeAll bool) ([]ifaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Name < ifaces[j].Name
	})
	ifaces = filterIfaces(ifaces, includeAll)

	details := make([]ifaceInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		addresses, err := ifaceAddresses(iface)
		if err != nil {
			return nil, err
		}

		mac := iface.HardwareAddr.String()
		if mac == "" {
			mac = "-"
		}

		details = append(details, ifaceInfo{
			Name:      iface.Name,
			Index:     iface.Index,
			MTU:       iface.MTU,
			MAC:       mac,
			Flags:     ifaceFlagLabels(iface.Flags),
			Addresses: addresses,
		})
	}

	return details, nil
}

func init() {
	rootCmd.AddCommand(listIfacesCmd)
	listIfacesCmd.Flags().BoolVar(
		&detailedIfaces,
		"detailed",
		false,
		"show detailed interface information",
	)
	listIfacesCmd.Flags().BoolVar(
		&jsonIfaces,
		"json",
		false,
		"output in JSON format",
	)
	listIfacesCmd.Flags().BoolVarP(
		&allIfaces,
		"all",
		"a",
		false,
		"output all interfaces, whether they are up or down",
	)
}

func printIfacesPlain(cmd *cobra.Command, ifaces []ifaceInfo, detailed bool) error {
	w := cmd.OutOrStdout()
	if !detailed {
		for _, iface := range ifaces {
			if _, err := fmt.Fprintln(w, iface.Name); err != nil {
				return err
			}
		}
		return nil
	}

	for idx, iface := range ifaces {
		if idx > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(
			w,
			"name: %s\nindex: %d\nmtu: %d\nmac: %s\nflags: %s\n",
			iface.Name,
			iface.Index,
			iface.MTU,
			iface.MAC,
			formatLabels(iface.Flags),
		); err != nil {
			return err
		}
		if len(iface.Addresses) == 0 {
			if _, err := fmt.Fprintln(w, "addresses: -"); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintln(w, "addresses:"); err != nil {
			return err
		}
		for _, address := range iface.Addresses {
			if _, err := fmt.Fprintf(w, "  - %s\n", address); err != nil {
				return err
			}
		}
	}
	return nil
}

func filterIfaces(ifaces []net.Interface, includeAll bool) []net.Interface {
	if includeAll {
		return ifaces
	}

	filtered := make([]net.Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 {
			filtered = append(filtered, iface)
		}
	}

	return filtered
}

func ifaceAddresses(iface net.Interface) ([]string, error) {
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("list addresses for %q: %w", iface.Name, err)
	}

	addresses := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		addresses = append(addresses, addr.String())
	}

	return addresses, nil
}

func printIfacesJSON(cmd *cobra.Command, ifaces []ifaceInfo, detailed bool) error {
	w := cmd.OutOrStdout()
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	if !detailed {
		names := make([]string, 0, len(ifaces))
		for _, iface := range ifaces {
			names = append(names, iface.Name)
		}
		return encoder.Encode(names)
	}

	return encoder.Encode(ifaces)
}

func formatLabels(labels []string) string {
	if len(labels) == 0 {
		return "-"
	}

	return strings.Join(labels, ",")
}

func ifaceFlagLabels(flags net.Flags) []string {
	parts := make([]string, 0, ifaceFlagsCapacity)
	if flags&net.FlagUp != 0 {
		parts = append(parts, "up")
	}
	if flags&net.FlagBroadcast != 0 {
		parts = append(parts, "broadcast")
	}
	if flags&net.FlagLoopback != 0 {
		parts = append(parts, "loopback")
	}
	if flags&net.FlagPointToPoint != 0 {
		parts = append(parts, "point-to-point")
	}
	if flags&net.FlagMulticast != 0 {
		parts = append(parts, "multicast")
	}
	if flags&net.FlagRunning != 0 {
		parts = append(parts, "running")
	}

	return parts
}
