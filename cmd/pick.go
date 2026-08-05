package cmd

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/discovery"
	"github.com/deppes/localsend-cli/internal/protocol"
	"github.com/deppes/localsend-cli/internal/tui"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

var pickCmd = &cobra.Command{
	Use:   "pick",
	Short: "Interactively pick a device and print its IP",
	RunE:  runPick,
}

func init() {
	rootCmd.AddCommand(pickCmd)
}

func runPick(_ *cobra.Command, _ []string) error {
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

	devCh := make(chan protocol.DiscoveredDevice, 32)
	onDevice := func(dev protocol.DiscoveredDevice) {
		select {
		case devCh <- dev:
		default:
		}
	}

	go discovery.ListenUDP(ctx, reg, self, filter, cfg.Favorites, onDevice) //nolint:errcheck
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
	go discovery.ReannounceToKnown(ctx, reg, self, 5*time.Second)

	model := tui.NewPicker(devCh)
	p := tea.NewProgram(model, tea.WithoutSignalHandler())
	result, err := p.Run()
	if err != nil {
		return err
	}

	m := result.(tui.PickerModel)
	if m.Selected != nil {
		fmt.Println(m.Selected.IP)
	}
	return nil
}
