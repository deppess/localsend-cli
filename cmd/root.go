package cmd

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/config"
	"github.com/deppes/localsend-cli/internal/discovery"
	"github.com/deppes/localsend-cli/internal/handlers"
	"github.com/deppes/localsend-cli/internal/protocol"
	tlsutil "github.com/deppes/localsend-cli/internal/tls"
	"github.com/deppes/localsend-cli/internal/whitelist"
)

var rootCmd = &cobra.Command{
	Use:   "localsend-cli",
	Short: "Headless LocalSend client for LAN file transfer",
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

var portOverride int

func init() {
	rootCmd.PersistentFlags().IntVar(&portOverride, "port", 0, "override configured port")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// startBaseServer starts an HTTPS server exposing /info and /register so that
// other devices can discover us. Used by pick and discover commands.
// The server is shut down when ctx is cancelled.
func startBaseServer(cfg *config.Config, self protocol.DeviceInfo, cert tls.Certificate, reg *discovery.Registry, filter *whitelist.Filter) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/localsend/v2/info", handlers.InfoHandler(self))
	mux.HandleFunc("/api/localsend/v2/register", discovery.RegisterHandler(reg, self, filter, cfg.Favorites, nil))

	srv := tlsutil.NewServer(fmt.Sprintf(":%d", cfg.Device.Port), mux, cert)
	srv.ErrorLog = log.New(io.Discard, "", 0) // suppress TLS noise from peer probes
	go func() {
		if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "base server:", err)
		}
	}()
}

// initCommon loads config, initialises TLS, and builds the self DeviceInfo.
func initCommon() (*config.Config, protocol.DeviceInfo, tls.Certificate, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, protocol.DeviceInfo{}, tls.Certificate{}, fmt.Errorf("load config: %w", err)
	}

	if portOverride > 0 {
		cfg.Device.Port = portOverride
	}

	cfgDir, err := config.Dir()
	if err != nil {
		return nil, protocol.DeviceInfo{}, tls.Certificate{}, err
	}

	cert, fp, err := tlsutil.LoadOrGenerate(cfgDir)
	if err != nil {
		return nil, protocol.DeviceInfo{}, tls.Certificate{}, fmt.Errorf("tls: %w", err)
	}

	if cfg.Device.Fingerprint != fp {
		cfg.Device.Fingerprint = fp
		config.Save(cfg) //nolint:errcheck
	}

	self := protocol.DeviceInfo{
		Alias:       cfg.Device.Name,
		Version:     protocol.Version,
		DeviceType:  cfg.Device.Type,
		Fingerprint: fp,
		Port:        cfg.Device.Port,
		Protocol:    "https",
		Download:    false,
		Announce:    true,
	}

	return cfg, self, cert, nil
}
