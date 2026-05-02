# Wormhole
**Wormhole allows you to expose local web servers sitting behind NATs or firewalls to the public internet securely, using multiplexed TCP tunnels. It is designed as a lightweight, single-binary alternative to tools like Ngrok or Cloudflare Tunnels.**

## The Wormhole CLI (Usage)
### 1. Build the binary.
```bash
    go build -o wormhole main.go
```

### 2. Start the server.
Run this on your cloud VPS or a central machine. It will open port 8080 for clients to connect, and port 9090 for web visitors.
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
Open your browser and navigate to: http://joshua.localhost:9090 (or your server's public IP/Domain).

