package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
)

func TestDebugEndpointReportsTunnelCounts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := NewTunnelServer("443", ctx)
	s.StartDebugServer("127.0.0.1:16060")
	time.Sleep(200 * time.Millisecond)

	// A reserved-but-unregistered subdomain must show up in Tunnels but not
	// in TunnelsActive: that gap is the leak signal the harness watches.
	s.acquireSubdomainIfAvailable("reserved-only")
	s.tunnels["registered"] = &yamux.Session{}

	resp, err := http.Get("http://127.0.0.1:16060/metrics")
	if err != nil {
		t.Fatalf("metrics endpoint unreachable: %v", err)
	}
	defer resp.Body.Close()

	var m DebugMetrics
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("bad metrics payload: %v", err)
	}

	if m.Tunnels != 2 {
		t.Errorf("Tunnels = %d, want 2", m.Tunnels)
	}
	if m.TunnelsActive != 1 {
		t.Errorf("TunnelsActive = %d, want 1", m.TunnelsActive)
	}
	if m.Goroutines <= 0 {
		t.Errorf("Goroutines = %d, want > 0", m.Goroutines)
	}
	if m.HeapAllocBytes == 0 {
		t.Error("HeapAllocBytes = 0, want > 0")
	}

	// pprof must be served from the same listener for the diagnosis workflow.
	pprofResp, err := http.Get("http://127.0.0.1:16060/debug/pprof/goroutine?debug=1")
	if err != nil {
		t.Fatalf("pprof unreachable: %v", err)
	}
	defer pprofResp.Body.Close()
	if pprofResp.StatusCode != http.StatusOK {
		t.Errorf("pprof status = %d, want 200", pprofResp.StatusCode)
	}
}
