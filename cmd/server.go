package cmd

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
	"github.com/spf13/cobra"
)

var (
	tunnelLock    sync.RWMutex
	activeTunnels = make(map[string]*yamux.Session)
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

		// Read the handshake before the Yamux session is established
		subdomain, err := readHandshake(conn)
		if err != nil {
			fmt.Println("Handshake failed:", err)
			conn.Close()
			continue
		}

		// check if the provided subdomain is already taken and reserve it if not.
		if takeSubdomain(string(subdomain)) {
			fmt.Println("Handshake rejected: subdomain ", subdomain, " is already taken")
			conn.Close()
			continue
		}

		fmt.Printf("Client identified as: %s\n", subdomain)

		// convert it into a yamux session
		session, err := yamux.Server(conn, nil)
		if err != nil {
			fmt.Println("Warning: invalid yamux handshake:", err)
			conn.Close() // Cleanup

			// free the reserved subdomain so others can use it
			tunnelLock.Lock()
			delete(activeTunnels, subdomain)
			tunnelLock.Unlock()

			continue
		}

		// Associate the subdomain with the session
		activeTunnels[subdomain] = session

		// cleanup watchdog
		go func(name string, sess *yamux.Session) {
			// This line freezes here until the client disconnects
			<-sess.CloseChan()

			fmt.Printf("Client %s disconnected. Freeing subdomain.\n", name)
			tunnelLock.Lock()
			delete(activeTunnels, name)
			tunnelLock.Unlock()

		}(subdomain, session)

		visitorListener, err := net.Listen("tcp", fmt.Sprintf(":%d", userPort))
		if err != nil {
			fmt.Println("Warning: Could not open public port:", err)
			session.Close() // Cleanup

			// free the reserved subdomain so others can use it
			tunnelLock.Lock()
			delete(activeTunnels, subdomain)
			tunnelLock.Unlock()

			continue
		}
		fmt.Println("Listening on port", userPort, "for visitors...")

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

// readHandshake reads exactly 1 byte at a time to prevent stealing Yamux data
func readHandshake(conn net.Conn) (string, error) {
	// the client has exactly 3 seconds to complete the handshake
	err := conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return "", err
	}

	// clear the deadline before returning so the deadline isnt applicable to Yamux
	defer conn.SetReadDeadline(time.Time{})

	var nameBytes []byte
	buffer := make([]byte, 1)

	for {
		_, err := conn.Read(buffer)
		if err != nil {
			return "", err // Catches timeouts and sudden drops
		}

		if buffer[0] == '\n' {
			break // Handshake complete!
		}

		nameBytes = append(nameBytes, buffer[0])

		// SIZE LIMIT: Subdomains can't be longer than 64 characters
		if len(nameBytes) > 64 {
			return "", fmt.Errorf("handshake rejected: name too long")
		}
	}

	return string(nameBytes), nil
}

func takeSubdomain(name string) bool {
	tunnelLock.Lock()
	defer tunnelLock.Unlock()
	if _, exists := activeTunnels[name]; exists {
		return true
	}

	// take the subdomain for this session (temporarily null)
	activeTunnels[name] = nil
	return false
}
