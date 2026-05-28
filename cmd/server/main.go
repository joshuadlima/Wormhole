package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/joshuadlima/Wormhole/internal/server"
	"github.com/spf13/cobra"
)

func main() {
	godotenv.Load()

	var rootCmd = &cobra.Command{
		Use:   "wormhole-server",
		Short: "Start the Wormhole server",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Starting Server...")

			// TODO: You can add your Redis initialization logic here later

			tunnelServer := server.NewTunnelServer("443")

			// Get the TLS certificate before starting the server to ensure it's ready to serve HTTPS traffic.
			tunnelServer.GetTLSCertificate()

			err := tunnelServer.Start()
			if err != nil {
				fmt.Println("Error starting server:", err)
			}
		},
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
