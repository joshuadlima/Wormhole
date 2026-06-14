package client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

var ErrSubdomainTaken = fmt.Errorf("ERROR 100: Subdomain is already taken.")

// Struct that holds the state of the client
type TunnelClient struct {
	Context     context.Context
	localPort   string
	subdomain   string
	serverHost  string
	TunnelReady chan string // Channel to signal when the tunnel is ready
	readyOnce   sync.Once
}

// Constructor
func NewTunnelClient(ctx context.Context, localPort string, subdomain string, serverHost string) *TunnelClient {
	return &TunnelClient{
		Context:     ctx,
		localPort:   localPort,
		subdomain:   subdomain,
		serverHost:  serverHost,
		TunnelReady: make(chan string, 1), // Buffered channel to avoid blocking
	}
}

// 3. We attach methods to the Struct (Notice the `s *TunnelClient` receiver)
func (s *TunnelClient) Start() error {
	attempt := 0
	maxBackoff := 30 * time.Second

	for {
		fmt.Printf("Attempting to connect (Try %d)...\n", attempt+1)

		connected, err := s.connectAndServe()

		if connected {
			attempt = 0 // Reset attempt counter on successful connection
		}

		// If the connection died or failed to start
		if err == ErrSubdomainTaken {
			return ErrSubdomainTaken
		} else if err != nil {
			fmt.Printf("ERROR 999: %s\n", err)
		} else {
			fmt.Println("Tunnel closed cleanly.")
			return nil
		}

		// Calculate Exponential Backoff
		attempt++

		// TODO - This guy might get real HUGE
		backoffDuration := time.Duration(1<<attempt) * time.Second

		if backoffDuration > maxBackoff {
			backoffDuration = maxBackoff
		}

		fmt.Printf("Reconnecting in %v...\n", backoffDuration)

		// Wait for either the backoff duration or a shutdown signal (used select to avoid blocking on time.Sleep and missing shutdown signals)
		select {
		case <-time.After(backoffDuration):
		case <-s.Context.Done():
			return nil
		}
	}
}

func (s *TunnelClient) connectAndServe() (bool, error) {
	// InsecureSkipVerify is set to true only for testing purposes.
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
	}

	dialer := &tls.Dialer{
		Config: tlsConfig,
	}

	// Dial an outbound connection to the server's port 4443
	serverConn, err := dialer.DialContext(s.Context, "tcp", s.serverHost+":4443")
	if err != nil {
		return false, err
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
	if err != nil {
		return false, err
	}

	// Read the server's response to the handshake - 2 way handshake to confirm the subdomain is available before proceeding.
	response, err := tunnel.ReadHandshake(serverConn)
	if err != nil {
		return false, err
	} else if response == "ERROR: Subdomain taken" {
		return false, ErrSubdomainTaken
	} else if response == "OK" {
		liveUrl := fmt.Sprintf("https://%s.%s/", s.subdomain, s.serverHost)
		fmt.Printf("Tunnel is ready at %s\n", liveUrl)
		// Signal that the tunnel is ready by sending the live URL through the channel (only once)
		s.readyOnce.Do(func() { s.TunnelReady <- liveUrl })
	}

	// convert the connection to a yamux client session
	config := yamux.DefaultConfig()
	config.EnableKeepAlive = true
	config.KeepAliveInterval = 30 * time.Second

	serverSession, err := yamux.Client(serverConn, config)
	if err != nil {
		return false, err
	}

	defer serverSession.Close() // Ensure the session is closed when this fxn exits

	fmt.Println("Converted to yamux client session...")

	for {
		// wait until the server side is ready then establish the stream
		serverStream, err := serverSession.Accept()
		if err != nil {
			return true, err
		}
		fmt.Println("Accepted new stream from server...")

		// dial in to the local server
		localdialer := &net.Dialer{}
		localConn, err := localdialer.DialContext(s.Context, "tcp", "localhost:"+s.localPort)
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
