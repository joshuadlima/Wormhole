package tunnel

import (
	"fmt"
	"net"
	"time"
)

// ReadHandshake reads exactly 1 byte at a time until '\n'to safely extract the handshake string
func ReadHandshake(conn net.Conn) (string, error) {
	err := conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return "", err
	}
	defer conn.SetReadDeadline(time.Time{})

	var nameBytes []byte
	buffer := make([]byte, 1)

	for {
		_, err := conn.Read(buffer)
		if err != nil {
			return "", err
		}

		if buffer[0] == '\n' {
			break
		}

		nameBytes = append(nameBytes, buffer[0])

		if len(nameBytes) > 64 {
			return "", fmt.Errorf("handshake rejected: payload too long")
		}
	}

	return string(nameBytes), nil
}
