package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/discovery"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Scan for devices on the local network",
	RunE:  runDiscover,
}

var discoverStream bool

func init() {
	discoverCmd.Flags().BoolVar(&discoverStream, "stream", false, "emit devices as JSON lines as they are found (runs until Ctrl+C)")
	rootCmd.AddCommand(discoverCmd)
}

func runDiscover(_ *cobra.Command, _ []string) error {
	cfg, self, cert, err := initCommon()
	if err != nil {
		return err
	}

	filter := whitelist.New(cfg.Whitelist.Enabled, cfg.Whitelist.IPs)
	reg := discovery.NewRegistry()

	// Serve /info and /register so other devices can discover us.
	startBaseServer(cfg, self, cert, reg, filter)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	enc := json.NewEncoder(os.Stdout)

	onDevice := func(dev protocol.DiscoveredDevice) {
		if discoverStream {
			enc.Encode(dev) //nolint:errcheck
		}
	}

	go discovery.ListenUDP(ctx, reg, self, cfg.Favorites, onDevice) //nolint:errcheck
	go func() {
		for {
			discovery.ScanHTTP(ctx, reg, self, filter, cfg.Favorites, onDevice)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	go discovery.BroadcastUDP(ctx, self)

	if discoverStream {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		return nil
	}

	timeout := time.Duration(cfg.Discovery.TimeoutMS) * time.Millisecond
	time.Sleep(timeout)
	cancel()

	devices := reg.List()
	if len(devices) == 0 {
		fmt.Fprintln(os.Stderr, "no devices found")
		return nil
	}
	return enc.Encode(devices)
}
