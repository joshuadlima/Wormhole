package main

import (
	"bufio"
	"fmt"
	"net"

	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

func main() {
	// 1. Establish the single Control Connection
	controlConn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		panic(err)
	}

	// 2. Identify this connection to the server as the Control channel
	controlConn.Write([]byte("CONTROL\n"))
	fmt.Println("Control channel established. Waiting for traffic...")

	// 3. Listen for commands from the server
	scanner := bufio.NewScanner(controlConn)
	for scanner.Scan() {
		command := scanner.Text()

		// When the server says "NEW_VISITOR", we spin up a data tunnel!
		if command == "NEW_VISITOR" {
			go createDataTunnel()
		}
	}
}

func createDataTunnel() {
	// Dial the server and identify as a DATA connection
	dataConn, _ := net.Dial("tcp", "localhost:8080")
	dataConn.Write([]byte("DATA\n"))

	// Dial the local Angular app
	localConn, _ := net.Dial("tcp", "localhost:4200")

	// Bridge them
	tunnel.BridgeConnections(dataConn, localConn)
}
