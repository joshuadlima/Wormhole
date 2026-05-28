package main

import (
	"fmt"
	"os"

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
			fmt.Println("Starting Client...")
			tunnelClient := client.NewTunnelClient(localPort, subdomain, serverHost)
			err := tunnelClient.Start()
			if err != nil {
				fmt.Println("Error starting client:", err)
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
