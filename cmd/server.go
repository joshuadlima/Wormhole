package cmd

import (
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Wormhole server",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting Server...")
		startServer()
	},
}

func init() {
	// "server" is a subcommand of the root
	rootCmd.AddCommand(serverCmd)
}

func startServer() {
	// Listen for a tcp connection on port 8080 to the client
	clientListener, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Listening on port 8080...")

	userPort := 9090

	for {
		// block until the client is available
		conn, err := clientListener.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println("Client connected on port 8080...")

		// convert it into a yamux session
		session, err := yamux.Server(conn, nil)
		if err != nil {
			fmt.Println("Warning: invalid yamux handshake:", err)
			conn.Close() // Cleanup
			continue
		}

		visitorListener, err := net.Listen("tcp", fmt.Sprintf(":%d", userPort))
		if err != nil {
			fmt.Println("Warning: Could not open public port:", err)
			session.Close() // Cleanup
			continue
		}
		fmt.Println("Listening on port", userPort , "for visitors...")

		go func(vl net.Listener, sess *yamux.Session, p int) {
			defer vl.Close()

			for {
				// wait for a user to connect to the public port 9090 & establish a tcp session once they do
				connUser, err := vl.Accept()
				if err != nil {
					fmt.Println(err)
					return
				}
				fmt.Println("Visitor connected on port ", p)
	
				// create a new yamux stream for this user connection
				clientStream, err := sess.Open()
				if err != nil {
					fmt.Println(err)
					return
				}
				fmt.Println("Yamux client stream opened for visitor...")
	
				// bridge the user connection to the client stream in a new goroutine so we can continue accepting new users
				go tunnel.BridgeConnections(connUser, clientStream)
			}
		}(visitorListener, session, userPort)

		// increment the port number
		userPort = userPort + 1
	}
}
