package tunnel

import (
	"io"
	"net"
)

func BridgeConnections(conn1 net.Conn, conn2 net.Conn) {
	defer conn1.Close()
	defer conn2.Close()

	go io.Copy(conn1, conn2)
	io.Copy(conn2, conn1)
}
