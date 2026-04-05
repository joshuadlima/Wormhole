package main

import (
	"fmt"
	"net"
)

func do(conn net.Conn) {
	fmt.Printf("New connection from %s\n", conn.RemoteAddr())
	buffer := make([]byte, 1024)
	_, err := conn.Read(buffer)
	if err != nil {
		fmt.Printf("Error reading from connection: %s\n", err)
	}

	conn.Write([]byte("HTTP/1.1 200 OK\r\n\r\nHello from tunnel server!\r\n"))

	conn.Close()
}

func main() {
	fmt.Println("Starting tunnel server...")

	lstnr, err := net.Listen("tcp", ":8080")

	for {
		if err != nil {
			fmt.Printf("Error starting server: %s\n", err)
			return
		} else {
			conn, err := lstnr.Accept()

			if err != nil {
				fmt.Printf("Error accepting connection: %s\n", err)
				return
			}

			do(conn)
		}
		defer lstnr.Close()
	}

}
