// Package cmd defines bifrost CLI commands.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var (
	detailedIfaces bool
	jsonIfaces     bool
)

const ifaceFlagsCapacity = 6

// listIfacesCmd prints network interfaces available on the local host.
var listIfacesCmd = &cobra.Command{
	Use:   "list-ifaces",
	Short: "List available network interfaces",
	RunE: func(cmd *cobra.Command, _ []string) error {
		ifaces, err := net.Interfaces()
		if err != nil {
			return fmt.Errorf("list interfaces: %w", err)
		}

		sort.Slice(ifaces, func(i, j int) bool {
			return ifaces[i].Name < ifaces[j].Name
		})

		if jsonIfaces {
			return printIfacesJSON(cmd, ifaces, detailedIfaces)
		}

		if detailedIfaces {
			return printDetailedIfaces(cmd, ifaces)
		}

		return printIfaceNames(cmd, ifaces)
	},
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
}

func printIfaceNames(cmd *cobra.Command, ifaces []net.Interface) error {
	w := cmd.OutOrStdout()
	for _, iface := range ifaces {
		if _, err := fmt.Fprintln(w, iface.Name); err != nil {
			return err
		}
	}

	return nil
}

// printDetailedIfaces prints a human-readable block for each interface.
func printDetailedIfaces(cmd *cobra.Command, ifaces []net.Interface) error {
	w := cmd.OutOrStdout()
	for idx := range ifaces {
		if err := printDetailedIface(w, ifaces[idx], idx > 0); err != nil {
			return err
		}
	}

	return nil
}

// printDetailedIface prints one interface block.
func printDetailedIface(writer io.Writer, iface net.Interface, withSeparator bool) error {
	if withSeparator {
		if _, err := fmt.Fprintln(writer); err != nil {
			return err
		}
	}

	if err := printIfaceHeader(writer, iface); err != nil {
		return err
	}
	return printIfaceAddresses(writer, iface)
}

// printIfaceHeader prints static interface metadata.
func printIfaceHeader(writer io.Writer, iface net.Interface) error {
	if _, err := fmt.Fprintf(writer, "name: %s\n", iface.Name); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "index: %d\n", iface.Index); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "mtu: %d\n", iface.MTU); err != nil {
		return err
	}

	mac := iface.HardwareAddr.String()
	if mac == "" {
		mac = "-"
	}
	if _, err := fmt.Fprintf(writer, "mac: %s\n", mac); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "flags: %s\n", formatFlags(iface.Flags)); err != nil {
		return err
	}

	return nil
}

// printIfaceAddresses prints all addresses bound to an interface.
func printIfaceAddresses(writer io.Writer, iface net.Interface) error {
	addresses, err := ifaceAddresses(iface)
	if err != nil {
		return err
	}

	if len(addresses) == 0 {
		_, printErr := fmt.Fprintln(writer, "addresses: -")
		return printErr
	}

	if _, err := fmt.Fprintln(writer, "addresses:"); err != nil {
		return err
	}

	for _, address := range addresses {
		if _, err := fmt.Fprintf(writer, "  - %s\n", address); err != nil {
			return err
		}
	}
	return nil
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

type ifaceDetailJSON struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MTU       int      `json:"mtu"`
	MAC       string   `json:"mac"`
	Flags     []string `json:"flags"`
	Addresses []string `json:"addresses"`
}

func printIfacesJSON(cmd *cobra.Command, ifaces []net.Interface, detailed bool) error {
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

	result := make([]ifaceDetailJSON, 0, len(ifaces))
	for _, iface := range ifaces {
		addresses, err := ifaceAddresses(iface)
		if err != nil {
			return err
		}

		result = append(result, ifaceDetailJSON{
			Name:      iface.Name,
			Index:     iface.Index,
			MTU:       iface.MTU,
			MAC:       iface.HardwareAddr.String(),
			Flags:     ifaceFlagLabels(iface.Flags),
			Addresses: addresses,
		})
	}

	return encoder.Encode(result)
}

// formatFlags converts net flags to comma-separated labels.
func formatFlags(flags net.Flags) string {
	parts := ifaceFlagLabels(flags)
	if len(parts) == 0 {
		return "-"
	}

	return strings.Join(parts, ",")
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
