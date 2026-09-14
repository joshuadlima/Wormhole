// Command loadtest measures Wormhole's control plane: how many concurrent
// tunnels the server sustains, how long each takes to come up under a
// controlled arrival rate, and whether the server gives the resources back
// when they all disconnect.

// It deliberately does not generate HTTP request load - that's the data
// plane, and k6 covers it (see tests/k6). The two stress different parts of
// the server, so saturating one tells you nothing about the other.

// Five phases: start a shared origin, ramp N tunnels at a fixed rate, hold
// them open while sampling the server, verify each one actually routes
// traffic (connected != working), then tear everything down and check the
// server returned to its pre-test baseline.

// go run ./cmd/loadtest -server example.com -tunnels 200 >/dev/null
package main

import (
	"context"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/joshuadlima/Wormhole/internal/client"
)

const (
	readyTimeout   = 30 * time.Second
	sampleInterval = time.Second
	settleTime     = 15 * time.Second
)

var logger = log.New(os.Stderr, "[loadtest] ", log.Ltime)

type config struct {
	serverHost  string
	tunnels     int
	spawnRate   float64
	hold        time.Duration
	originPort  string
	originSize  int
	originDelay time.Duration
	metricsURL  string
	outDir      string
	label       string
}

// tunnelResult is one simulated client's outcome, from dial through teardown.
type tunnelResult struct {
	subdomain string
	url       string
	readyIn   time.Duration
	connected bool // handshake completed and the tunnel reported ready
	verified  bool // an HTTP request actually routed through it end to end
	err       string
	cancel    context.CancelFunc
}

// serverMetrics mirrors server.DebugMetrics, redeclared here so the harness
// doesn't need to import the server package to decode its JSON.
type serverMetrics struct {
	Goroutines     int    `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	Tunnels        int    `json:"tunnels"`
	TunnelsActive  int    `json:"tunnels_active"`
}

// sample is one server-side reading, tagged with elapsed time and phase so it
// can be lined up against the client-side results afterwards.
type sample struct {
	elapsed time.Duration
	phase   string
	metrics serverMetrics
}

func main() {
	cfg := parseFlags()

	runDir := filepath.Join(cfg.outDir, fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), cfg.label))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		logger.Fatalf("cannot create results directory: %v", err)
	}
	logger.Printf("results -> %s", runDir)

	// Phase 0: the shared backend every tunnel points at.
	startOrigin(cfg.originPort, cfg.originSize, cfg.originDelay)
	logger.Printf("origin listening on :%s (%d byte body, %v delay)", cfg.originPort, cfg.originSize, cfg.originDelay)

	baseline, err := fetchMetrics(cfg.metricsURL)
	if err != nil {
		logger.Fatalf("cannot reach server metrics at %s: %v\n"+
			"Start the server with --debug-addr and, if it is remote, forward the port:\n"+
			"  ssh -L 6060:localhost:6060 <host>", cfg.metricsURL, err)
	}
	logger.Printf("baseline: %d goroutines, %s heap, %d tunnels",
		baseline.Goroutines, humanBytes(baseline.HeapAllocBytes), baseline.Tunnels)

	// Sampler: a ticker appending to samples in its own goroutine. Nothing
	// else touches the slice until stopDone is closed, so no locking needed
	// there - but phase is read here and written from main(), so it goes
	// through atomic.Value rather than a plain string.
	var samples []sample
	var phase atomic.Value
	phase.Store("ramp")
	start := time.Now()
	stop := make(chan struct{})
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		ticker := time.NewTicker(sampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if m, err := fetchMetrics(cfg.metricsURL); err == nil {
					samples = append(samples, sample{elapsed: time.Since(start), phase: phase.Load().(string), metrics: m})
				}
			case <-stop:
				return
			}
		}
	}()

	// Phase 1: ramp.
	results := ramp(cfg)

	// Phase 2: hold.
	phase.Store("hold")
	logger.Printf("holding %d tunnels for %v", countConnected(results), cfg.hold)
	time.Sleep(cfg.hold)

	// Phase 3: verify.
	phase.Store("verify")
	verify(results)

	// Phase 4: teardown.
	phase.Store("teardown")
	logger.Printf("tearing down %d tunnels", countConnected(results))
	for _, r := range results {
		if r.cancel != nil {
			r.cancel()
		}
	}

	phase.Store("settle")
	logger.Printf("settling for %v before the final reading", settleTime)
	time.Sleep(settleTime)

	final, err := fetchMetrics(cfg.metricsURL)
	if err != nil {
		logger.Printf("warning: final metrics read failed: %v", err)
	}

	close(stop)
	<-stopDone

	writeResultsCSV(filepath.Join(runDir, "tunnels.csv"), results)
	writeMetricsCSV(filepath.Join(runDir, "metrics.csv"), samples)

	report(cfg, results, baseline, final, runDir)
}

func parseFlags() config {
	var cfg config

	flag.StringVar(&cfg.serverHost, "server", "localhost", "Wormhole server host to tunnel through")
	flag.IntVar(&cfg.tunnels, "tunnels", 100, "Number of concurrent tunnels to establish")
	flag.Float64Var(&cfg.spawnRate, "spawn-rate", 10, "Tunnels to start per second during the ramp")
	flag.DurationVar(&cfg.hold, "hold", 60*time.Second, "How long to hold every tunnel open once the ramp finishes")
	flag.StringVar(&cfg.originPort, "origin-port", "8080", "Local port for the shared origin service")
	flag.IntVar(&cfg.originSize, "origin-size", 1024, "Origin response body size in bytes")
	flag.DurationVar(&cfg.originDelay, "origin-delay", 0, "Artificial per-request delay at the origin")
	flag.StringVar(&cfg.metricsURL, "metrics-url", "http://127.0.0.1:6060/metrics", "Server diagnostics endpoint to sample")
	flag.StringVar(&cfg.outDir, "out", "loadtest-results", "Directory to write results into")
	flag.StringVar(&cfg.label, "label", "run", "Short label for this run, used in the results directory name")
	flag.Parse()

	if cfg.spawnRate <= 0 || cfg.tunnels <= 0 {
		logger.Fatal("-spawn-rate and -tunnels must be greater than zero")
	}
	return cfg
}

// startOrigin brings up the local service every simulated client exposes.
// One origin serves all of them, since TunnelClient only ever dials
// localhost:<port>. Response size and delay are knobs: a fast, small body
// isolates the tunnel's own overhead, while added delay simulates a real
// backend and shifts the test towards concurrency handling.
func startOrigin(port string, bodySize int, delay time.Duration) {
	body := strings.Repeat("x", bodySize)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		fmt.Fprint(w, body)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	go http.ListenAndServe(":"+port, mux)
	time.Sleep(250 * time.Millisecond) // let a bind failure surface here, not N tunnels later
}

// ramp starts tunnels at a fixed arrival rate and records how long each takes
// to report ready. The ticker paces spawns; each client then runs in its own
// goroutine so one slow handshake doesn't delay the rest of the ramp.
func ramp(cfg config) []tunnelResult {
	results := make([]tunnelResult, cfg.tunnels)
	done := make(chan struct{}, cfg.tunnels)

	interval := time.Duration(float64(time.Second) / cfg.spawnRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.Printf("ramping %d tunnels at %.1f/s (~%v)", cfg.tunnels, cfg.spawnRate,
		time.Duration(float64(cfg.tunnels)/cfg.spawnRate*float64(time.Second)).Round(time.Second))

	for i := 0; i < cfg.tunnels; i++ {
		<-ticker.C
		go func(index int) {
			results[index] = spawnTunnel(cfg)
			done <- struct{}{}
		}(i)
		if (i+1)%50 == 0 {
			logger.Printf("  spawned %d/%d", i+1, cfg.tunnels)
		}
	}
	for i := 0; i < cfg.tunnels; i++ {
		<-done
	}

	logger.Printf("ramp complete: %d/%d tunnels up", countConnected(results), cfg.tunnels)
	return results
}

// spawnTunnel brings up one client and waits for it to report ready, fail, or
// time out.
func spawnTunnel(cfg config) tunnelResult {
	ctx, cancel := context.WithCancel(context.Background())
	result := tunnelResult{cancel: cancel}

	tunnelClient := client.NewTunnelClient(ctx, cfg.originPort, "", cfg.serverHost)
	errCh := make(chan error, 1)
	started := time.Now()
	go func() { errCh <- tunnelClient.Start() }()

	select {
	case liveURL := <-tunnelClient.TunnelReady:
		result.readyIn = time.Since(started)
		result.connected = true
		result.url = liveURL
		if parsed, err := url.Parse(liveURL); err == nil {
			result.subdomain = strings.SplitN(parsed.Hostname(), ".", 2)[0]
		}
	case err := <-errCh:
		result.err = "start failed"
		if err != nil {
			result.err = err.Error()
		}
		cancel()
	case <-time.After(readyTimeout):
		result.err = fmt.Sprintf("timed out after %v", readyTimeout)
		cancel()
	}
	return result
}

// verify sends one request through every connected tunnel. A completed
// handshake only proves the client reached the server - it says nothing
// about whether the routing map and origin actually line up, so without this
// a run can report N tunnels while some fraction silently 404.
func verify(results []tunnelResult) {
	httpClient := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}

	verified := 0
	for i := range results {
		r := &results[i]
		if !r.connected {
			continue
		}
		resp, err := httpClient.Get(strings.TrimSuffix(r.url, "/") + "/healthz")
		if err != nil {
			r.err = "verify: " + err.Error()
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			r.verified = true
			verified++
		} else {
			r.err = fmt.Sprintf("verify: HTTP %d", resp.StatusCode)
		}
	}
	logger.Printf("verified %d/%d tunnels actually serve traffic", verified, countConnected(results))
}

func fetchMetrics(metricsURL string) (serverMetrics, error) {
	var m serverMetrics
	resp, err := http.Get(metricsURL)
	if err != nil {
		return m, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return m, fmt.Errorf("metrics endpoint returned %s", resp.Status)
	}
	return m, json.NewDecoder(resp.Body).Decode(&m)
}

func report(cfg config, results []tunnelResult, baseline, final serverMetrics, runDir string) {
	connected := countConnected(results)
	verified := 0
	var readyTimes []time.Duration
	for _, r := range results {
		if r.verified {
			verified++
		}
		if r.connected {
			readyTimes = append(readyTimes, r.readyIn)
		}
	}
	sort.Slice(readyTimes, func(i, j int) bool { return readyTimes[i] < readyTimes[j] })

	rule := strings.Repeat("=", 60)
	fmt.Fprintf(os.Stderr, "\n%s\n  Wormhole control-plane load test - %s\n%s\n\n", rule, cfg.label, rule)
	fmt.Fprintf(os.Stderr, "server            %s\n", cfg.serverHost)
	fmt.Fprintf(os.Stderr, "requested         %d tunnels at %.1f/s\n", cfg.tunnels, cfg.spawnRate)
	fmt.Fprintf(os.Stderr, "connected         %d/%d (%.1f%%)\n", connected, cfg.tunnels, pct(connected, cfg.tunnels))
	fmt.Fprintf(os.Stderr, "verified serving  %d/%d (%.1f%%)\n\n", verified, cfg.tunnels, pct(verified, cfg.tunnels))

	fmt.Fprintf(os.Stderr, "time to ready     p50 %v   p95 %v   p99 %v   max %v\n\n",
		percentile(readyTimes, 50).Round(time.Millisecond),
		percentile(readyTimes, 95).Round(time.Millisecond),
		percentile(readyTimes, 99).Round(time.Millisecond),
		maxOf(readyTimes).Round(time.Millisecond))

	fmt.Fprintf(os.Stderr, "server resources       baseline -> final    delta\n")
	fmt.Fprintf(os.Stderr, "  goroutines           %8d -> %-8d %+d\n", baseline.Goroutines, final.Goroutines, final.Goroutines-baseline.Goroutines)
	fmt.Fprintf(os.Stderr, "  heap                 %8s -> %-8s\n", humanBytes(baseline.HeapAllocBytes), humanBytes(final.HeapAllocBytes))
	fmt.Fprintf(os.Stderr, "  tunnels in map       %8d -> %-8d %+d\n\n", baseline.Tunnels, final.Tunnels, final.Tunnels-baseline.Tunnels)

	goroutineDelta := final.Goroutines - baseline.Goroutines
	tunnelDelta := final.Tunnels - baseline.Tunnels
	if goroutineDelta > 10 || tunnelDelta > 0 {
		pprofURL := strings.Replace(cfg.metricsURL, "/metrics", "/debug/pprof/goroutine", 1)
		fmt.Fprintf(os.Stderr, "leak check: FAIL - server did not return to baseline.\n")
		fmt.Fprintf(os.Stderr, "  inspect with: go tool pprof %s\n", pprofURL)
	} else {
		fmt.Fprintf(os.Stderr, "leak check: PASS - goroutines and routing map returned to baseline.\n")
	}

	fmt.Fprintf(os.Stderr, "\nresults written to %s\n%s\n", runDir, rule)
}

func writeResultsCSV(path string, results []tunnelResult) {
	file, err := os.Create(path)
	if err != nil {
		logger.Printf("warning: could not write %s: %v", path, err)
		return
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()
	w.Write([]string{"subdomain", "url", "ready_ms", "connected", "verified", "error"})
	for _, r := range results {
		w.Write([]string{
			r.subdomain, r.url,
			fmt.Sprintf("%.2f", float64(r.readyIn.Microseconds())/1000),
			fmt.Sprint(r.connected), fmt.Sprint(r.verified), r.err,
		})
	}
}

func writeMetricsCSV(path string, samples []sample) {
	file, err := os.Create(path)
	if err != nil {
		logger.Printf("warning: could not write %s: %v", path, err)
		return
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()
	w.Write([]string{"elapsed_seconds", "phase", "goroutines", "heap_alloc_bytes", "tunnels", "tunnels_active"})
	for _, s := range samples {
		w.Write([]string{
			fmt.Sprintf("%.3f", s.elapsed.Seconds()), s.phase,
			fmt.Sprint(s.metrics.Goroutines), fmt.Sprint(s.metrics.HeapAllocBytes),
			fmt.Sprint(s.metrics.Tunnels), fmt.Sprint(s.metrics.TunnelsActive),
		})
	}
}

func countConnected(results []tunnelResult) int {
	n := 0
	for _, r := range results {
		if r.connected {
			n++
		}
	}
	return n
}

// percentile returns the p-th percentile using nearest-rank, on an
// already-sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(p / 100 * float64(len(sorted)))
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func maxOf(sorted []time.Duration) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[len(sorted)-1]
}

func pct(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGT"[exp])
}
