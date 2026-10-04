package tunnel

import (
	"io"
	"net"
)

// BridgeConnections copies data both ways until either side finishes, then
// closes both. Closing on the first direction to end matters: if the local
// app drops an idle keep-alive connection, the stream must close too, or the
// server's pooled connection looks alive and the next request is sent into a
// dead pipe.
func BridgeConnections(conn1 net.Conn, conn2 net.Conn) {
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(conn1, conn2)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(conn2, conn1)
		done <- struct{}{}
	}()

	<-done
	conn1.Close()
	conn2.Close()
	<-done
}
