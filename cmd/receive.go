package cmd

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/config"
	"github.com/deppes/localsend-cli/internal/discovery"
	"github.com/deppes/localsend-cli/internal/handlers"
	"github.com/deppes/localsend-cli/internal/tui"
	"github.com/deppes/localsend-cli/internal/whitelist"
	tlsutil "github.com/deppes/localsend-cli/internal/tls"
)

var receiveCmd = &cobra.Command{
	Use:   "receive",
	Short: "Receive files from other devices",
	RunE:  runReceive,
}

var receiveDir string
var receiveHeadless bool

func init() {
	receiveCmd.Flags().StringVar(&receiveDir, "dir", "", "directory to save received files (default: config value)")
	receiveCmd.Flags().BoolVar(&receiveHeadless, "headless", false, "no TUI; exit after one transfer session and print received filenames to stdout")
	rootCmd.AddCommand(receiveCmd)
}

func runReceive(_ *cobra.Command, _ []string) error {
	cfg, self, tlsCert, err := initCommon()
	if err != nil {
		return err
	}

	if receiveDir != "" {
		cfg.Receive.Dir = receiveDir
	}

	if receiveHeadless && !cfg.Whitelist.Enabled {
		fmt.Fprintln(os.Stderr, "warning: --headless with whitelist disabled accepts files from any device on the network")
	}

	filter := whitelist.New(cfg.Whitelist.Enabled, cfg.Whitelist.IPs)
	reg := discovery.NewRegistry()

	var tuiNotifier *tui.ReceiveNotifier
	var hlNotifier *headlessNotifier
	var h *handlers.Handler

	if receiveHeadless {
		hlNotifier = newHeadlessNotifier()
		h = handlers.New(cfg, filter, hlNotifier)
	} else {
		tuiNotifier = tui.NewReceiveNotifier()
		h = handlers.New(cfg, filter, tuiNotifier)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/localsend/v2/info", handlers.InfoHandler(self, filter))
	mux.HandleFunc("/api/localsend/v2/register", discovery.RegisterHandler(reg, self, filter, cfg.Favorites, nil))
	mux.HandleFunc("/api/localsend/v2/prepare-upload", h.PrepareUpload)
	mux.HandleFunc("/api/localsend/v2/upload", h.Upload)
	mux.HandleFunc("/api/localsend/v2/cancel", h.CancelHandler)

	server := tlsutil.NewServer(fmt.Sprintf(":%d", cfg.Device.Port), mux, tlsCert)
	server.ErrorLog = log.New(io.Discard, "", 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; cancel(); server.Close() }()

	go func() {
		if err := server.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "server error:", err)
		}
	}()

	go discovery.BroadcastUDP(ctx, self)
	go discovery.ListenUDP(ctx, reg, self, cfg.Favorites, nil) //nolint:errcheck
	go discovery.ReannounceToKnown(ctx, reg, self, 5*time.Second)
	go func() {
		for {
			discovery.ScanHTTP(ctx, reg, self, filter, cfg.Favorites, nil)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()

	dir := config.ExpandDir(cfg.Receive.Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create receive dir: %w", err)
	}

	if receiveHeadless {
		var result handlers.SessionResult
		select {
		case result = <-hlNotifier.done:
		case <-ctx.Done():
			server.Close()
			return nil
		}
		cancel()
		server.Close()

		if len(result.Files) == 0 {
			return nil
		}
		fmt.Printf("TOTAL:%d\n", result.Total)
		for _, f := range result.Files {
			fmt.Println(f)
		}
		return nil
	}

	model := tui.NewReceiveView(tuiNotifier.Channel())
	p := tea.NewProgram(model, tea.WithoutSignalHandler())
	res, err := p.Run()
	if err != nil {
		return err
	}

	m := res.(tui.ReceiveModel)
	if m.Err != nil {
		return m.Err
	}
	return nil
}

// headlessNotifier auto-accepts all incoming transfers and signals when the session completes.
type headlessNotifier struct {
	done chan handlers.SessionResult
}

func newHeadlessNotifier() *headlessNotifier {
	return &headlessNotifier{done: make(chan handlers.SessionResult, 1)}
}

func (n *headlessNotifier) Incoming(_ context.Context, t handlers.IncomingTransfer) bool {
	go func() {
		result := <-t.Done
		select {
		case n.done <- result:
		default:
		}
	}()
	return true
}
