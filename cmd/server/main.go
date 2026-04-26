package main

import (
	"fmt"
	"net"

	"github.com/hashicorp/yamux"
	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

func main() {
	// Listen for a tcp connection on port 8080 to the client
	clientListener, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on port 8080...")

	// block until the client is available
	conn, err := clientListener.Accept()
	if err != nil {
		panic(err)
	}
	fmt.Println("Client connected on port 8080...")

	// convert it into a yamux session
	session, err := yamux.Server(conn, nil)
	if err != nil {
		panic(err)
	}

	visitorListener, err := net.Listen("tcp", ":9090")
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on port 9090 for visitors...")

	for {
		// wait for a user to connect to the public port 9090 & establish a tcp session once they do
		connUser, err := visitorListener.Accept()
		if err != nil {
			panic(err)
		}
		fmt.Println("Visitor connected on port 9090...")

		// create a new yamux stream for this user connection
		clientStream, err := session.Open()
		if err != nil {
			panic(err)
		}
		fmt.Println("Yamux client stream opened for visitor...")

		// bridge the user connection to the client stream in a new goroutine so we can continue accepting new users
		go tunnel.BridgeConnections(connUser, clientStream)
	}
}
