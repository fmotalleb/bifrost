package cmd

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var detailedIfaces bool

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

func printDetailedIfaces(cmd *cobra.Command, ifaces []net.Interface) error {
	w := cmd.OutOrStdout()
	for idx, iface := range ifaces {
		if idx > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}

		if _, err := fmt.Fprintf(w, "name: %s\n", iface.Name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "index: %d\n", iface.Index); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "mtu: %d\n", iface.MTU); err != nil {
			return err
		}

		mac := iface.HardwareAddr.String()
		if mac == "" {
			mac = "-"
		}
		if _, err := fmt.Fprintf(w, "mac: %s\n", mac); err != nil {
			return err
		}

		if _, err := fmt.Fprintf(w, "flags: %s\n", formatFlags(iface.Flags)); err != nil {
			return err
		}

		addrs, err := iface.Addrs()
		if err != nil {
			return fmt.Errorf("list addresses for %q: %w", iface.Name, err)
		}

		if len(addrs) == 0 {
			if _, err := fmt.Fprintln(w, "addresses: -"); err != nil {
				return err
			}
			continue
		}

		if _, err := fmt.Fprintln(w, "addresses:"); err != nil {
			return err
		}

		for _, addr := range addrs {
			if _, err := fmt.Fprintf(w, "  - %s\n", addr.String()); err != nil {
				return err
			}
		}
	}

	return nil
}

func formatFlags(flags net.Flags) string {
	parts := make([]string, 0, 6)
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

	if len(parts) == 0 {
		return "-"
	}

	return strings.Join(parts, ",")
}
