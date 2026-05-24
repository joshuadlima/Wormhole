package cmd

import (
	"fmt"

	"github.com/joshuadlima/Wormhole/internal/client"
	"github.com/spf13/cobra"
)

var localPort string
var subdomain string
var serverHost string

var clientCmd = &cobra.Command{
	Use:   "client",
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

func init() {
	// "client" is a subcommand of the root
	rootCmd.AddCommand(clientCmd)

	clientCmd.Flags().StringVarP(&localPort, "local", "l", "", "Local port to expose. (required) eg. 4200)")
	clientCmd.Flags().StringVarP(&subdomain, "subdomain", "sd", "", "Subdomain to use. (randomized if not provided) eg. myapp)")
	clientCmd.Flags().StringVarP(&serverHost, "server", "sh", "localhost", "Server host to connect to eg. myserver.com")

	clientCmd.MarkFlagRequired("local")
}
