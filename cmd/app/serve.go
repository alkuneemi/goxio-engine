package main

import "github.com/spf13/cobra"

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "serve",
		Short:        "Run the HTTP server with auto-discovery and live updates.",
		Long:         "serve starts the GoXIO HTTP server on :8080 (or $PORT). It runs Docker and Kubernetes auto-discovery, a read-only disk/volume/container collector (GOXIO_SPACE_SOURCE=auto|host|demo), applies any YAML-declared services, and streams real-time health updates over /websocket. Space actions are confirm-gated and do not write to the host.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config")
			logLevel, _ := cmd.Flags().GetString("log-level")
			logFormat, _ := cmd.Flags().GetString("log-format")
			return runServe(configPath, logLevel, logFormat)
		},
	}
}
