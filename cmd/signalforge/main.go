// SignalForge — automated problem discovery engine.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/moontechs/signalforge/internal/cli"
)

const version = "0.1.0"

var rootCmd = &cobra.Command{
	Use:   "signalforge",
	Short: "SignalForge — automated problem discovery engine",
	Long: `SignalForge collects public signals from GitHub, Hacker News, and Stack Exchange,
classifies them, clusters recurring problems, and generates evidence-backed product hypotheses.`,
	Version: version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Set up signal handling for graceful shutdown.
		ctx, cancel := context.WithCancel(context.Background())
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-c
			cancel()
			fmt.Fprintln(os.Stderr, "\nShutting down...")
			os.Exit(0)
		}()
		// Store context for commands that need it.
		cmd.SetContext(ctx)
		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.AddCommand(cli.InitCmd)
	rootCmd.AddCommand(cli.DoctorCmd)
	rootCmd.AddCommand(cli.CollectCmd)
	rootCmd.AddCommand(cli.ListCmd)
	rootCmd.AddCommand(cli.ShowCmd)
	rootCmd.AddCommand(cli.AnalyzeCmd)
	rootCmd.AddCommand(cli.BrainstormCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
