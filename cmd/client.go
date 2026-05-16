package cmd

import (
	"fmt"

	"github.com/joshuadlima/Wormhole/internal/client"
	"github.com/spf13/cobra"
)

var localPort string
var subdomain string

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Start the Wormhole client",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting Client...")
		tunnelClient := client.NewTunnelClient(localPort, subdomain)
		err := tunnelClient.Start()
		if err != nil {
			fmt.Println("Error starting client:", err)
		}
	},
}

func init() {
	// "client" is a subcommand of the root
	rootCmd.AddCommand(clientCmd)

	clientCmd.Flags().StringVarP(&localPort, "local", "l", "", "Local port to expose. (required)")
	clientCmd.Flags().StringVarP(&subdomain, "subdomain", "s", "", "Subdomain to use. (randomized if not provided)")

	clientCmd.MarkFlagRequired("local")
}
