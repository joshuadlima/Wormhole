package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/joshuadlima/Wormhole/internal/server"
	"github.com/spf13/cobra"
)

func main() {
	godotenv.Load()

	var debugAddr string

	var rootCmd = &cobra.Command{
		Use:   "wormhole-server",
		Short: "Start the Wormhole server",
		Run: func(cmd *cobra.Command, args []string) {
			// Context to listen for interrupt signals to allow graceful shutdown
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel() // Ensure cleanup when we exit
			errCh := make(chan error, 1)

			fmt.Println("Starting Server...")

			// TODO: Add Redis initialization logic here

			tunnelServer := server.NewTunnelServer("443", ctx)

			// Configure the ACME defaults before starting the server to ensure it's ready to serve HTTPS traffic.
			tunnelServer.ConfigureACMEDefaults()

			// Diagnostics for load testing. Loopback only by default - pprof
			// must not be publicly reachable.
			if debugAddr != "" {
				tunnelServer.StartDebugServer(debugAddr)
			}

			go func() {
				errCh <- tunnelServer.Start()
			}()

			select {
			case <-tunnelServer.ServerReady:
				fmt.Println("Server is ready to accept connections.")
			case err := <-errCh:
				if err != nil {
					fmt.Println("Error starting server:", err)
					os.Exit(1)
				}
				return
			}

			// Phase 2: stay up until the server actually stops.
			if err := <-errCh; err != nil {
				fmt.Println("Server stopped:", err)
				os.Exit(1)
			}
			fmt.Println("Shut down gracefully.")
		},
	}

	rootCmd.Flags().StringVar(&debugAddr, "debug-addr", "127.0.0.1:6060",
		"Address for the /metrics and pprof diagnostics listener. Keep this on loopback; empty disables it.")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
