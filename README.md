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
-- Architecture diagram -- 

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
- Adding TLS is important for a reverse-tunnel since several web features, packages, and services wouldn't work on an insecure page.
- To get a certificate for the VPS, we can go for the HTTP_01 challenge or the DNS_01 challenge.
- HTTP_01 will require a new certificate for each subdomain, and this will hit the certificate per domain rate limits of the Certificate Authority on scale. (50/week for Let's Encrypt)
- DNS_01 gets us a wildcard certificate valid for all subdomains, but it works by adding an entry into the domain provider's DNS record, accessing it via the provider's API (eg. Cloudflare)
