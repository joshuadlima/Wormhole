package cmd

import (
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
	"github.com/spf13/cobra"
)

var localPort string
var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Start the Wormhole client",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting Client...")
		startClient()
	},
}

func init() {
	// "client" is a subcommand of the root
	rootCmd.AddCommand(clientCmd)

	// setting the client command flag for the local port to expose
	clientCmd.Flags().StringVarP(&localPort, "local", "l", "4200", "Local port to expose")
}

func startClient() {
	// Dial an outbound connection to the server's port 8080
	serverConn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Dialed in to server on outbound port 8080...")

	subdomain := "joshua"
	_, err = serverConn.Write([]byte(subdomain + "\n"))
	if err != nil {
		fmt.Println(err)
		return
	}

	// convert the connection to a yamux client session
	serverSession, err := yamux.Client(serverConn, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Converted to yamux client session...")

	for {
		// wait until the server side is ready then establish the stream
		serverStream, err := serverSession.Accept()
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println("Accepted new stream from server...")

		// dial in to the local server
		localConn, err := net.Dial("tcp", "localhost:"+localPort)
		if err != nil {
			fmt.Println("Local server is down!", err)
			serverStream.Close() // Hang up on the visitor
			continue             // Keep the Wormhole Client alive!
		}
		fmt.Println("Dialed in to local server on port " + localPort + "...")

		// bridge the server stream to the local connection
		go tunnel.BridgeConnections(serverStream, localConn)
	}
}
