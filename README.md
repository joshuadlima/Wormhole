# Wormhole
**Wormhole allows you to expose local web servers sitting behind NATs or firewalls to the public internet securely. It is designed as a lightweight, single-binary alternative to tools like Ngrok or Cloudflare Tunnels.**

## Installation
Download the latest standalone binary for your system directly from the links below.
* **Windows:** [Download wormhole-windows-amd64.exe](https://github.com/joshuadlima/Wormhole/releases/latest/download/wormhole-windows-amd64.exe)
* **Mac (Apple Silicon):** [Download wormhole-mac-silicon](https://github.com/joshuadlima/Wormhole/releases/latest/download/wormhole-mac-silicon)
* **Mac (Intel):** [Download wormhole-mac-intel](https://github.com/joshuadlima/Wormhole/releases/latest/download/wormhole-mac-intel)
* **Linux:** [Download wormhole-linux-amd64](https://github.com/joshuadlima/Wormhole/releases/latest/download/wormhole-linux-amd64)

## The Wormhole CLI (Usage)
### 1. Clone the repo and build the binary, or install the latest binary directly from the links above.
```bash
    git clone https://github.com/joshuadlima/Wormhole.git
    cd Wormhole
    go build -o wormhole main.go
```

### 2. Start the server.
Run this on your cloud VPS or a central machine. It will open port 4443 for clients to connect, and port 443 for web visitors.
```bash
    ./wormhole server
```

### 3. Start the client.
Run this on your local laptop to expose a local web app (e.g., running on port 4200) to the public internet.
If you leave the --subdomain flag blank, Wormhole will automatically generate a secure, random 32-character hex string for you.
```bash
    ./wormhole client --subdomain joshua --local 4200
```

### 4. Visit your app!
Open your browser and navigate to: http://joshua.localhost:443 (or your server's public IP/Domain).

## Design & Architecture
<img width="1584" height="1232" alt="Wormhole diagrams(1)" src="https://github.com/user-attachments/assets/a6bfe769-fbd9-402b-bafc-dfd6244928e6" />

### 1. TCP Multiplexing (Yamux)
- Packs thousands of logical streams over a single network connection. Without multiplexing, we would need to establish several individual TCP network connections, which could result in resource exhaustion.

### 2. Busybox pattern
- Both the Client and Server logic are bundled into a single executable using Cobra. This is designed for self-hosting and ease of use.
- Future scope: server flag to enable Redis store and authentication for a similar design as Ngrok or Cloudflare tunnels

### 3. HTTP Reverse Proxy (Layer 7)
- The Server concurrently runs an HTTP Listener (default Port 443).
- When a web visitor makes an HTTP request, the router parses the Host header, extracts the subdomain, looks up the corresponding yamux session in a thread-safe map, and opens a new binary stream.
- Instead of opening a new internet connection, the httputil.ReverseProxy is injected with a custom DialContext that forces all plain-text HTTP data directly down the multiplexed Yamux stream.

### 4. TLS certificate issuance
- Adding TLS is important for a reverse-tunnel since several web features, packages, and services wouldn't work on an insecure page. The certmagic package has been used to automate the obtaining and renewal of TLS certificates.

<details>
<summary>Click to dive deeper.</summary>

  <ol>
    <li>
      <strong>Understanding the types of challenges and their working.</strong>
      <ul>
        <li>To get a certificate for the VPS, we can go for the HTTP_01 challenge or the DNS_01 challenge (there are others as well).</li>
        <li>HTTP_01 will require a new certificate for each subdomain, and this will hit the certificate per-domain rate limits of the Certificate Authority on scale. (50/week for Let's Encrypt).</li>
        <li>DNS_01 gets us a wildcard certificate valid for all subdomains, but it works by adding an entry into the domain provider's DNS record, accessing it via the provider's API (eg. Cloudflare). Since an access of this kind would require administrator privileges, it serves as a good proof of domain ownership. </li>
      </ul>
    </li>
  </ol>
  
</details>


### 5. Client-side backoff logic & Ghost connection handling
- Several network hiccups and internet issues could cause the TCP connection to drop. In such cases, we shouldn't make the client reconnect manually. We thus wrap the connection logic in a loop with a exponential backoff retry mechanism so that the connection can retry and fix itself.
- In cases where the internet or wifi suddenly drops, the client might not get a chance to communicate the disconnect with the VPS, and the connection will still exist as a Ghost connection. We thus use yamux keepalives (ping-pong mechanism) on both the client and the server end so that the connections are always in sync.


### 6. Testing Plan (real pain)
<img width="3456" height="1024" alt="Wormhole diagrams(4)" src="https://github.com/user-attachments/assets/dbff950d-2733-4a8c-9e03-cbfba2475fdd" />
- The straightforward plan was to test the number of concurrent tunnels a standard server can handle and how much data can flow through it.
- This is achieved by a custom Go script to ramp up and do a load test for active tunnels and k6 for the data flow test.
- The load test and the main server were run of seperate VPSs in the same region (to reduce network latency and noise)


<details>
<summary>Transport Bug & Goroutine leak Discovery</summary>
<strong>Snippet of metrics.csv after running a test for 200 tunnels with ramp of 10</strong>
<p>command: go run ./cmd/loadtest -server server.xyz -tunnels 200 -spawn-rate 10 -label cap-200 >/dev/null</p>

| elapsed_seconds | phase | goroutines | heap_alloc_bytes | tunnels | tunnels_active |
| :--- | :--- | :--- | :--- | :--- | :--- |
| 76.002 | hold | 812 | 4922616 | 200 | 200 |
| 77.002 | hold | 812 | 4934616 | 200 | 200 |
| 78.003 | hold | 812 | 4946616 | 200 | 200 |
| 79.003 | hold | 812 | 4958616 | 200 | 200 |
| 80.003 | hold | 812 | 4970616 | 200 | 200 |
| 81.002 | verify | 876 | 6338264 | 200 | 200 |
| 82.002 | verify | 954 | 8034816 | 200 | 200 |
| 83.002 | verify | 1036 | 9533648 | 200 | 200 |
| 84.003 | verify | 1118 | 10835960 | 200 | 200 |
| 85.002 | verify | 1198 | 11418168 | 200 | 200 |

<strong>Observation - steady increase in goroutines and heap size followed by each verify log.</strong>
<p>Verify simply hits /healthz on the tunnel via server:443 and waits for an "ok" acknowledgement on the client side to make sure data is flowing correctly. This shouldn't leave any hanging goroutines or cause a permanent increase in heap size.</p>

<strong>Deeper analysis - get a snapshot of running goroutines after firing several verify calls.</strong>
<p>I first spun the main server on VPS A, then a load test server on VPS B. Started a single tunnel via the load test harness running: </p>

```bash
root@wormhole-test-server:~# go run ./cmd/loadtest -server 211111112.xyz -tunnels 1 -hold 30m -label probe
```

<p>Made 100 sequential calls to /healthz on the active tunnel in order to generate enough goroutine evidence for further debugging.</p>

```bash
root@wormhole-test-server:~# for i in $(seq 100); do curl -sk https://ca1ce9ffde6f2ef9e7e118f99b3dc7e0.server.xyz/healthz >/dev/null; done
```

<p>Checked the snapshot of top 80 lines of active goroutines in the system by running:</p>

```bash
root@wormhole-main-server:~# curl -s 'localhost:6060/debug/pprof/goroutine?debug=1' | head -80
```

<strong>Output (truncated):</strong>

```bash
goroutine profile: total 216
100 @ 0x48b0ce 0x467705 0x6fbbe6 0x492ae1
#       0x6fbbe5        net/http.(*persistConn).writeLoop+0xe5  /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/http/transport.go:2652

100 @ 0x48b0ce 0x467705 0x8471a6 0x6f9727 0x6680e3 0x668213 0x6fa232 0x492ae1
#       0x8471a5        github.com/hashicorp/yamux.(*Stream).Read+0x3c5 /root/go/pkg/mod/github.com/hashicorp/yamux@v0.1.2/stream.go:147
#       0x6f9726        net/http.(*persistConn).Read+0x46               /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/http/transport.go:2174
#       0x6680e2        bufio.(*Reader).fill+0x102                      /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/bufio/bufio.go:113
#       0x668212        bufio.(*Reader).Peek+0x52                       /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/bufio/bufio.go:152
#       0x6fa231        net/http.(*persistConn).readLoop+0x171          /root/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/http/transport.go:2330
```

<strong>This verifies the goroutine leak, it happens for each request (thus 100 readloop + 100 writeloop goroutines)</strong>
<p>The stack trace makes it clear that the source is due to http.Transport connections running over Yamux streams for each request and this happens in one place in our server code: the ServeHTTP() method which is responsible to route incoming traffic to the respective Yamux tunnel. The culprit is the ReverseProxy creation code in this method, here I create a new http.Transport for each incoming request. Problematic code: </p>

```go
	// Create a HTTP Reverse Proxy to forward the request to the correct Yamux stream
	httpReverseProxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			// Ensure the request looks like a standard HTTP request before sending it down the tunnel
			req.Out.URL.Scheme = "http"
			req.Out.URL.Host = r.Host
		},
		Transport: &http.Transport{
			// Custom DialContext to route the HTTP request through the Yamux session instead of the normal network stack
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return session.Open()
			},
		},
	}
	httpReverseProxy.ServeHTTP(w, r)
```

<p>Why is this an issue? In golang http.Transport is a connection pool, and it's designed to be created once and reused. Wormhole creates a new one on every request and then abandons it. Each abandoned pool still holds an open connection, and each open connection has two goroutines waiting on it that will never be woken up. To make matters worse, IdleConnTimeout is 0 by default and so these goroutines just keep adding up. The fix:

1. One ReverseProxy/Transport per tunnel. Create it when the tunnel registers, store it in the routing map next to the yamux.Session, and have ServeHTTP look it up and use it.
2. Set IdleConnTimeout (for example 90 seconds, the same as the standard library's default) so idle streams close eventually.
3. Call CloseIdleConnections() in yamuxSessionCleanup when the tunnel goes away.
4. Make the bridge close both directions when either one finishes.

</p>

</details>
