package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/joshuadlima/Wormhole/internal/client"
	"github.com/spf13/cobra"
)

func main() {
	godotenv.Load()

	var localPort string
	var subdomain string
	var serverHost string

	var rootCmd = &cobra.Command{
		Use:   "wormhole-client",
		Short: "Start the Wormhole client",
		Run: func(cmd *cobra.Command, args []string) {
			// Context to listen for interrupt signals to allow graceful shutdown
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel() // Ensure cleanup when we exit

			tunnelClient := client.NewTunnelClient(ctx, localPort, subdomain, serverHost)
			errCh := make(chan error, 1)

			go func() {
				errCh <- tunnelClient.Start()
			}()

			// Phase 1: first success, fatal failure, or user shutdown — whichever comes first.
			select {
			case url := <-tunnelClient.TunnelReady:
				fmt.Println("Tunnel is up on " + url + ". Press Ctrl+C to stop.")
			case err := <-errCh:
				// Start() returned before we ever came up => it gave up.
				if err != nil {
					fmt.Println("Failed to start tunnel:", err)
				}
				return
			case <-ctx.Done():
				fmt.Println("\nShutting down gracefully...")
				return
			}

			// Phase 2: stay up until shutdown, or until the client gives up for good.
			select {
			case <-ctx.Done():
				fmt.Println("\nShutting down gracefully...")
			case err := <-errCh:
				if err != nil {
					fmt.Println("\nTunnel stopped:", err)
				}
			}
		},
	}

	rootCmd.Flags().StringVarP(&localPort, "local", "l", "", "Local port to expose (required). eg. 4200")
	rootCmd.Flags().StringVarP(&subdomain, "subdomain", "s", "", "Subdomain to use (randomized if not provided). eg. myapp")
	rootCmd.Flags().StringVarP(&serverHost, "server", "r", "localhost", "Server host to connect to. eg. myserver.com")
	rootCmd.MarkFlagRequired("local")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
