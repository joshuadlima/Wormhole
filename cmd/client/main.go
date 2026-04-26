package main

import (
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

func main() {
	// Dial an outbound connection to the server's port 8080
	serverConn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		panic(err)
	}
	fmt.Println("Dialed in to server on outbound port 8080...")

	// convert the connection to a yamux client session
	serverSession, err := yamux.Client(serverConn, nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("Converted to yamux client session...")

	for {
		// wait until the server side is ready then establish the stream
		serverStream, err := serverSession.Accept()
		if err != nil {
			panic(err)
		}
		fmt.Println("Accepted new stream from server...")

		// dial in to the local server
		localConn, err := net.Dial("tcp", "localhost:4200")
		if err != nil {
			panic(err)
		}
		fmt.Println("Dialed in to local server on port 4200...")

		// bridge the server stream to the local connection
		go tunnel.BridgeConnections(serverStream, localConn)
	}
}
