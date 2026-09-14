package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"
)

var processStart = time.Now()

// DebugMetrics is the JSON payload served by the debug listener on /metrics.
// It is deliberately a flat bag of counters and nothing else: the load-test
// sampler polls this every second for the whole run, so it has to stay cheap
// enough that measuring the server doesn't perturb the server.
//
// For diagnosis (which goroutines, allocated where) use the pprof endpoints
// on the same listener instead - those are expensive and meant to be pulled
// by hand, a handful of times, not on a timer.
type DebugMetrics struct {
	TimestampUnixMs int64   `json:"timestamp_unix_ms"`
	UptimeSeconds   float64 `json:"uptime_seconds"`

	Goroutines     int    `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapObjects    uint64 `json:"heap_objects"`
	SysBytes       uint64 `json:"sys_bytes"`
	NumGC          uint32 `json:"num_gc"`

	// Tunnels is every entry in the routing map, including subdomains that
	// have been reserved by a handshake but whose yamux session has not been
	// registered yet. TunnelsActive counts only the ones actually serving.
	// A persistent gap between the two means reservations are leaking.
	Tunnels       int `json:"tunnels"`
	TunnelsActive int `json:"tunnels_active"`
}

// tunnelCounts reports the size of the routing map and how much of it is live.
func (s *TunnelServer) tunnelCounts() (total int, active int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, session := range s.tunnels {
		total++
		if session != nil {
			active++
		}
	}
	return total, active
}

// Snapshot samples the server's current resource usage. Exported so tests and
// the load harness can read the same numbers the HTTP endpoint reports.
func (s *TunnelServer) Snapshot() DebugMetrics {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	total, active := s.tunnelCounts()

	return DebugMetrics{
		TimestampUnixMs: time.Now().UnixMilli(),
		UptimeSeconds:   time.Since(processStart).Seconds(),
		Goroutines:      runtime.NumGoroutine(),
		HeapAllocBytes:  mem.HeapAlloc,
		HeapObjects:     mem.HeapObjects,
		SysBytes:        mem.Sys,
		NumGC:           mem.NumGC,
		Tunnels:         total,
		TunnelsActive:   active,
	}
}

// StartDebugServer brings up the diagnostics listener: /metrics for the
// time-series sampler and the standard pprof handlers for heap and goroutine
// dumps.
//
// Bind this to a loopback address. pprof will happily hand a heap dump to
// anyone who asks, so it must never face the public internet - reach it from
// a workstation with `ssh -L 6060:localhost:6060 <host>` instead.
func (s *TunnelServer) StartDebugServer(addr string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.Snapshot())
	})

	// pprof.Index dispatches the named profiles (/debug/pprof/goroutine,
	// /heap, /allocs...); the rest need their own routes.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	debugServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-s.ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		debugServer.Shutdown(shutdownCtx)
	}()

	go func() {
		fmt.Println("Debug listener started on", addr, "(/metrics, /debug/pprof/)")
		if err := debugServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// The debug listener is not load-bearing, so a failure here must
			// not take the tunnel server down with it.
			fmt.Println("Warning: debug listener stopped:", err)
		}
	}()
}
