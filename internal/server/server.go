package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
	"github.com/libdns/cloudflare"
)

// tunnelEntry is one registered tunnel: its yamux session and the reverse
// proxy that forwards requests down it. The proxy's Transport is a connection
// pool, so it must live as long as the tunnel - building one per request
// abandoned a pooled connection (an open stream and two goroutines) on every
// request until the session closed.
type tunnelEntry struct {
	session   *yamux.Session
	proxy     *httputil.ReverseProxy
	transport *http.Transport
}

func newTunnelEntry(session *yamux.Session) *tunnelEntry {
	transport := &http.Transport{
		// Custom DialContext to route the HTTP request through the Yamux session instead of the normal network stack
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return session.Open()
		},
		// Every request to a tunnel shares one pool key, so this caps how many
		// idle streams a tunnel keeps; extras are closed when returned.
		MaxIdleConnsPerHost: 32,
		// A zero-value Transport never expires idle connections.
		IdleConnTimeout: 90 * time.Second,
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			// Ensure the request looks like a standard HTTP request before sending it down the tunnel
			req.Out.URL.Scheme = "http"
			req.Out.URL.Host = req.In.Host
		},
		Transport: transport,
	}

	return &tunnelEntry{session: session, proxy: proxy, transport: transport}
}

// Struct that holds the state of the server and its tunnels
type TunnelServer struct {
	mu          sync.RWMutex
	tunnels     map[string]*tunnelEntry // nil value = subdomain reserved, session not yet registered
	publicPort  string
	ctx         context.Context
	ServerReady chan string // Channel to signal when the server is ready to accept connections
	readyOnce   sync.Once
}

// Constructor
func NewTunnelServer(publicPort string, ctx context.Context) *TunnelServer {
	return &TunnelServer{
		tunnels:     make(map[string]*tunnelEntry),
		publicPort:  publicPort,
		ctx:         ctx,
		ServerReady: make(chan string),
	}
}

func (s *TunnelServer) Start() error {
	// 1. Get the TLS configuration from CertMagic for the domains
	tlsConfig, err := certmagic.TLS(strings.Split(os.Getenv("DOMAINS"), ","))

	if err != nil {
		return err
	}

	// listener, err := net.Listen("tcp", ":4443")
	listener, err := tls.Listen("tcp", ":4443", tlsConfig)
	if err != nil {
		return err
	}
	fmt.Println("Secure server started on port 4443...")

	// Start the HTTPS server in a separate goroutine to handle incoming web traffic and route it to the correct tunnels based on subdomain.
	webServer := &http.Server{
		Addr:              ":443",
		Handler:           s,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-s.ctx.Done()
		fmt.Println("Shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		webServer.Shutdown(shutdownCtx) // drains in-flight HTTP requests
		listener.Close()                // unblocks Accept below
	}()

	go func() {
		if err := webServer.ListenAndServeTLS("", ""); err == http.ErrServerClosed {
			fmt.Println("Web server stopped:", err)
		} else if err != nil {
			fmt.Println("Web server crashed:", err)
			listener.Close() // half a server is useless, bring the whole thing down
		}
	}()

	s.readyOnce.Do(func() {
		s.ServerReady <- "Server is ready"
	})

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil // listener is gone; stop
			}
			fmt.Println("Warning: transient accept error:",
			 err)
			continue
		}

		// To handle each client connection in a new goroutine so we can continue accepting new clients
		go s.handleClient(conn)

	}
}

// Handles 1 client connection from start to finish
func (s *TunnelServer) handleClient(conn net.Conn) {

	// Read the handshake before the Yamux session is established
	conn.SetReadDeadline(time.Now().Add(10 * time.Second)) // Set a read deadline for the handshake
	subdomain, err := tunnel.ReadHandshake(conn)
	conn.SetReadDeadline(time.Time{}) // Clear the read deadline after the handshake
	if err != nil {
		fmt.Println("Handshake failed:", err)
		conn.Close()
		return
	}

	// Check if the provided subdomain is already taken and reserve it if not.
	if !s.acquireSubdomainIfAvailable(subdomain) {
		conn.Write([]byte("ERROR: Subdomain taken\n"))
		conn.Close()
		return
	} else {
		conn.Write([]byte("OK\n"))
		fmt.Printf("Client identified as: %s\n", subdomain)
	}

	// Yamux setup
	config := yamux.DefaultConfig()
	config.EnableKeepAlive = true
	config.KeepAliveInterval = 30 * time.Second

	yamuxSession, err := yamux.Server(conn, config)
	if err != nil {
		conn.Write([]byte("SERVER: Error - Invalid yamux handshake\n"))
		fmt.Println("Warning: invalid yamux handshake:", err)
		conn.Close() // Cleanup

		// free the reserved subdomain so others can use it
		s.freeSubdomain(subdomain)

		return
	}

	// Associate the subdomain with the session
	entry := s.registerSubdomain(subdomain, yamuxSession)

	// Yamux session cleanup watchdog - frees the subdomain when the client disconnects
	go s.yamuxSessionCleanup(subdomain, entry)
}

// ServeHTTP routes incoming web traffic to the correct Yamux tunnel
// ServeHTTP is a special method that allows our TunnelServer struct to satisfy the http.Handler interface,
// which means we can use it to handle HTTP requests directly in our http.Server setup.
// This enables the use of ListenAndServe with our TunnelServer to route incoming HTTP requests to the correct tunnels based on subdomain.
func (s *TunnelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract the subdomain from the Host header
	hostParts := strings.Split(r.Host, ".")
	if len(hostParts) < 3 {
		http.Error(w, "Invalid host header", http.StatusBadRequest)
		return
	}
	subdomain := hostParts[0]

	// Check if a Yamux session exists for the subdomain
	s.mu.RLock()
	entry, exists := s.tunnels[subdomain]
	s.mu.RUnlock()

	if !exists || entry == nil {
		http.Error(w, fmt.Sprintf("Tunnel '%s' not found or offline", subdomain), http.StatusNotFound)
		return
	}

	entry.proxy.ServeHTTP(w, r)
}

// Checks if the subdomain is available and reserves it if it is
func (s *TunnelServer) acquireSubdomainIfAvailable(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tunnels[name]; exists {
		return false
	}

	// reserve the subdomain with a placeholder value until the session is established
	s.tunnels[name] = nil
	return true
}

// Registers the subdomain with the actual session once it's established
func (s *TunnelServer) registerSubdomain(name string, session *yamux.Session) *tunnelEntry {
	entry := newTunnelEntry(session)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnels[name] = entry
	return entry
}

// Frees the subdomain so others can use it once the client disconnects
func (s *TunnelServer) freeSubdomain(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tunnels, name)
}

// Helper function to watch for session closure and free the subdomain
func (s *TunnelServer) yamuxSessionCleanup(name string, entry *tunnelEntry) {
	<-entry.session.CloseChan()

	fmt.Printf("Client %s disconnected. Freeing subdomain.\n", name)
	s.freeSubdomain(name)
	entry.transport.CloseIdleConnections()
}

func (s *TunnelServer) ConfigureACMEDefaults() {
	// Initialize the DNS Provider with its API credentials (generate an API token with DNS Edit permissions)
	provider := &cloudflare.Provider{
		APIToken: os.Getenv("CLOUDFLARE_API_TOKEN"),
	}

	// Tell CertMagic to use the DNS-01 challenge and pass it the provider.
	certmagic.DefaultACME.DNS01Solver = &certmagic.DNS01Solver{
		DNSManager: certmagic.DNSManager{
			DNSProvider: provider,
		},
	}

	// Set the email (Required by Let's Encrypt for expiry notices/account recovery)
	certmagic.DefaultACME.Email = os.Getenv("EMAIL_ID")

	if os.Getenv("PRODUCTION") == "true" {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptProductionCA
	} else {
		certmagic.DefaultACME.CA = certmagic.LetsEncryptStagingCA
	}
}
