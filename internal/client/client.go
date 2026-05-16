package client

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

// Struct that holds the state of the client
type TunnelClient struct {
	localPort string
	subdomain string
}

// Constructor
func NewTunnelClient(localPort string, subdomain string) *TunnelClient {
	return &TunnelClient{
		localPort: localPort,
		subdomain: subdomain,
	}
}

// 3. We attach methods to the Struct (Notice the `s *TunnelClient` receiver)
func (s *TunnelClient) Start() error {

	// Dial an outbound connection to the server's port 4443
	serverConn, err := net.Dial("tcp", "localhost:4443")
	if err != nil {
		return err
	}
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
		fmt.Println("Your client is live on: http://" + s.subdomain + ".localhost:443/")
	}

	// convert the connection to a yamux client session
	serverSession, err := yamux.Client(serverConn, nil)
	if err != nil {
		return err
	}
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
