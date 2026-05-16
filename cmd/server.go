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
		err := tunnelServer.Start()
		if err != nil {
			fmt.Println(err)
		}
	},
}

func init() {
	// "server" is a subcommand of the root
	rootCmd.AddCommand(serverCmd)
}
