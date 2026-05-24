package client

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

// Struct that holds the state of the client
type TunnelClient struct {
	localPort  string
	subdomain  string
	serverHost string
}

// Constructor
func NewTunnelClient(localPort string, subdomain string, serverHost string) *TunnelClient {
	return &TunnelClient{
		localPort:  localPort,
		subdomain:  subdomain,
		serverHost: serverHost,
	}
}

// 3. We attach methods to the Struct (Notice the `s *TunnelClient` receiver)
func (s *TunnelClient) Start() error {
	attempt := 0
	maxBackoff := 30 * time.Second

	for {
		fmt.Printf("Attempting to connect (Try %d)...\n", attempt+1)

		err := s.connectAndServe()

		// If the connection died or failed to start
		if err != nil {
			fmt.Printf("Tunnel disconnected: %v\n", err)
		} else {
			fmt.Println("Tunnel closed cleanly.")
		}

		// Calculate Exponential Backoff
		attempt++
		backoffDuration := time.Duration(1<<attempt) * time.Second

		if backoffDuration > maxBackoff {
			backoffDuration = maxBackoff
		}

		fmt.Printf("Reconnecting in %v...\n", backoffDuration)
		time.Sleep(backoffDuration)
	}
}

func (s *TunnelClient) connectAndServe() error {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	// Dial an outbound connection to the server's port 4443
	serverConn, err := tls.Dial("tcp", s.serverHost+":4443", tlsConfig)
	if err != nil {
		return err
	}

	// Ensure the connection is closed when this fxn exits
	defer serverConn.Close()

	fmt.Println("Dialed in to server on outbound port 4443...")

	// Randomize the subdomain if not provided by the user
	if s.subdomain == "" {
		s.subdomain = s.generateRandomName()
	}

	// Send the subdomain as a handshake to the server before the Yamux session is established
	_, err = serverConn.Write([]byte(s.subdomain + "\n"))

	// Read the server's response to the handshake - 2 way handshake to confirm the subdomain is available before proceeding.
	response, err := tunnel.ReadHandshake(serverConn)
	if err != nil {
		return err
	} else if response == "ERROR: Subdomain taken" {
		fmt.Println("Bummer! That subdomain is already in use. Try another one.")
		return nil
	} else if response == "OK" {
		fmt.Println("Your client is live on: http://" + s.subdomain + "." + s.serverHost + ":443/")
	}

	// convert the connection to a yamux client session
	serverSession, err := yamux.Client(serverConn, nil)
	if err != nil {
		return err
	}

	defer serverSession.Close() // Ensure the session is closed when this fxn exits

	fmt.Println("Converted to yamux client session...")

	for {
		// wait until the server side is ready then establish the stream
		serverStream, err := serverSession.Accept()
		if err != nil {
			return err
		}
		fmt.Println("Accepted new stream from server...")

		// dial in to the local server
		localConn, err := net.Dial("tcp", "localhost:"+s.localPort)
		if err != nil {
			fmt.Println("Local server is down!", err)
			serverStream.Close() // Hang up on the visitor
			continue             // Keep the Wormhole Client alive!
		}
		fmt.Println("Dialed in to local server on port " + s.localPort + "...")

		// bridge the server stream to the local connection
		go tunnel.BridgeConnections(serverStream, localConn)
	}
}

func (s *TunnelClient) generateRandomName() string {
	bytes := make([]byte, 16) // 16 bytes = 32 hex characters
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
