package cmd

import (
	"fmt"

	"github.com/joshuadlima/Wormhole/internal/server"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Wormhole server",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting Server...")

		tunnelServer := server.NewTunnelServer("443")
		// Get the TLS certificate before starting the server to ensure it's ready to serve HTTPS traffic.
		err1 := tunnelServer.GetTLSCertificate()
		if err1 != nil {
			fmt.Println("Error getting TLS certificate:", err1)
			return
		}

		err2 := tunnelServer.Start()
		if err2 != nil {
			fmt.Println("Error starting server:", err2)
		}
	},
}

func init() {
	// "server" is a subcommand of the root
	rootCmd.AddCommand(serverCmd)
}
