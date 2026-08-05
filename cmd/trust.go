package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/config"
)

var trustCmd = &cobra.Command{
	Use:   "trust",
	Short: "Manage trusted (TOFU-pinned) peer fingerprints",
}

var trustListCmd = &cobra.Command{
	Use:   "list",
	Short: "List trusted peer IPs and fingerprints",
	RunE:  runTrustList,
}

var trustRemoveCmd = &cobra.Command{
	Use:   "remove <ip>",
	Short: "Remove a pinned fingerprint (forces re-TOFU on next contact)",
	Args:  cobra.ExactArgs(1),
	RunE:  runTrustRemove,
}

func init() {
	trustCmd.AddCommand(trustListCmd, trustRemoveCmd)
	rootCmd.AddCommand(trustCmd)
}

func runTrustList(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	snap := cfg.TrustedSnapshot()
	if len(snap) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no trusted peers")
		return nil
	}
	ips := make([]string, 0, len(snap))
	for ip := range snap {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	for _, ip := range ips {
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", ip, snap[ip])
	}
	return nil
}

func runTrustRemove(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	removed, err := cfg.RemoveTrusted(args[0])
	if err != nil {
		return err
	}
	if !removed {
		fmt.Fprintf(cmd.OutOrStdout(), "no trust entry for %s\n", args[0])
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "removed trust entry for %s\n", args[0])
	return nil
}
