package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/deppes/localsend-cli/internal/handlers"
)

var sendCmd = &cobra.Command{
	Use:   "send --to <ip> <file> [<file>...]",
	Short: "Send files or directories to a device",
	RunE:  runSend,
}

var sendTo string

func init() {
	sendCmd.Flags().StringVar(&sendTo, "to", "", "target device IP (required)")
	sendCmd.MarkFlagRequired("to")
	rootCmd.AddCommand(sendCmd)
}

func runSend(_ *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("at least one file or directory required")
	}

	for _, p := range args {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("path not found: %s", p)
		}
	}

	cfg, self, _, err := initCommon()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() { <-sig; cancel() }()

	logf := func(msg string) {
		fmt.Fprintln(os.Stderr, msg)
	}

	return handlers.SendFiles(ctx, handlers.SendOptions{
		TargetIP: sendTo,
		Paths:    args,
		Self:     self,
		Cfg:      cfg,
		OnLog:    logf,
	})
}
