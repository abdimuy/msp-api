// Package main is the winback entry point: the always-on binary that runs
// the WhatsApp channel (internal/canal) on a Linux VPS with a stable public
// HTTPS URL. Meta pushes webhooks — there is nothing to poll — so this
// binary must be reachable at all times, unlike the store's own server,
// which is started by hand and tunnels through a rotating URL. See
// docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md.
//
// Run subcommands:
//
//	winback serve      run the canal HTTP edge (webhook + internal API).
//	winback version    print build metadata and exit.
package main

import (
	"fmt"
	"os"

	// Embed the IANA tz database in the binary: the VPS this runs on may
	// ship no system zoneinfo, and firebird.BusinessTZ panics at runtime
	// without it. Same practice as cmd/api/main.go.
	_ "time/tzdata"

	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

// Build metadata, populated via -ldflags at compile time. See Makefile.
var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:   "winback",
		Short: "winback: always-on edge for the WhatsApp channel (internal/canal)",
	}
	root.AddCommand(serveCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build metadata and exit",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Printf("winback %s (built %s)\n", version, buildTime)
			return err
		},
	}
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the canal HTTP edge (webhook + internal API)",
		RunE: func(_ *cobra.Command, _ []string) error {
			fx.New(appOptions()...).Run()
			return nil
		},
	}
}
