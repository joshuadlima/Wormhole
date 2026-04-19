package main

import (
	"bufio"
	"fmt"
	"net"

	"github.com/joshuadlima/Wormhole/internal/tunnel"
)

func main() {
	// Channels to route incoming connections from port 8080
	controlChan := make(chan net.Conn)
	dataChan := make(chan net.Conn)

	go listenForClient(controlChan, dataChan)
	go listenForPublic(controlChan, dataChan)

	// Keep main thread alive
	select {}
}

func listenForClient(controlChan chan net.Conn, dataChan chan net.Conn) {
	listener, _ := net.Listen("tcp", ":8080")

	for {
		conn, _ := listener.Accept()

		// Read the handshake to see what kind of connection this is
		reader := bufio.NewReader(conn)
		handshake, _ := reader.ReadString('\n')

		switch handshake {
		case "CONTROL\n":
			controlChan <- conn
			fmt.Println("Client control connection registered.")
		case "DATA\n":
			dataChan <- conn
		}
	}
}

func listenForPublic(controlChan chan net.Conn, dataChan chan net.Conn) {
	listener, _ := net.Listen("tcp", ":9090")

	// Wait for the client to register its control connection before accepting public traffic
	controlConn := <-controlChan

	for {
		// A user's web browser hits 9090
		publicConn, _ := listener.Accept()

		// 1. Tell the client to open a new data connection
		controlConn.Write([]byte("NEW_VISITOR\n"))

		// 2. Wait for the client's new DATA connection to arrive on port 8080
		dataConn := <-dataChan

		// 3. Bridge the public user to the new data tunnel
		go tunnel.BridgeConnections(publicConn, dataConn)
	}
}
