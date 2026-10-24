#!/usr/bin/env bash
# Interactive launcher for a self-hosted Wormhole server (Linux / macOS).
# Creates .env on first run, installs Go if missing, builds and starts the server.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"

ENV_FILE=.env
BIN=bin/wormhole-server
REQUIRED_KEYS=(CLOUDFLARE_API_TOKEN EMAIL_ID DOMAINS SERVER_HOST PRODUCTION)

# ask <label> [default] [secret]  -> prints the answer on stdout, re-prompts until non-empty
ask() {
  local label=$1 default=${2-} secret=${3-} prompt reply
  prompt="$label"
  [ -n "$default" ] && prompt="$prompt [$default]"
  prompt="$prompt: "
  while true; do
    printf '%s' "$prompt" >&2
    if [ -n "$secret" ]; then
      read -rs reply
      echo >&2
    else
      read -r reply
    fi
    reply=${reply:-$default}
    [ -n "$reply" ] && break
    echo "  This field is required." >&2
  done
  printf '%s' "$reply"
}

env_is_complete() {
  local key
  [ -f "$ENV_FILE" ] || return 1
  for key in "${REQUIRED_KEYS[@]}"; do
    grep -Eq "^${key}=.+" "$ENV_FILE" || return 1
  done
}

configure_env() {
  local token email domains host first prod answer

  echo
  echo "Wormhole needs a wildcard TLS certificate, issued through the Cloudflare DNS-01 challenge."
  echo "Create a Cloudflare API token with 'Zone > DNS > Edit' permission for your domain."
  echo
  token=$(ask "CLOUDFLARE_API_TOKEN (input hidden)" "" secret)
  email=$(ask "EMAIL_ID (Let's Encrypt expiry notices)")
  domains=$(ask "DOMAINS, comma separated (e.g. example.com,*.example.com)")
  domains=${domains// /}

  first=${domains%%,*}
  first=${first#\*.}
  host=$(ask "SERVER_HOST" "$first")

  echo
  echo "Staging certificates are NOT trusted by browsers. Use them while testing to avoid Let's Encrypt rate limits."
  while true; do
    printf 'Use Let'\''s Encrypt production certificates? [y/N]: ' >&2
    read -r answer
    case ${answer:-n} in
      [Yy]*) prod=true; break ;;
      [Nn]*) prod=false; break ;;
    esac
  done

  (
    umask 077
    cat > "$ENV_FILE" <<EOF
CLOUDFLARE_API_TOKEN=$token
EMAIL_ID=$email
DOMAINS=$domains
SERVER_HOST=$host
PRODUCTION=$prod
EOF
  )
  echo "Wrote $ENV_FILE"
}

# Go >= 1.21 is enough: it fetches the exact toolchain named in go.mod on its own.
go_is_usable() {
  local minor
  command -v go >/dev/null 2>&1 || return 1
  minor=$(go env GOVERSION | sed -E 's/^go1\.([0-9]+).*/\1/')
  [[ $minor =~ ^[0-9]+$ ]] && [ "$minor" -ge 21 ]
}

install_go() {
  local os arch version
  case "$(uname -s)" in
    Linux)  os=linux ;;
    Darwin) os=darwin ;;
    *) echo "Unsupported OS. Install Go from https://go.dev/dl/ and re-run." >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "Unsupported CPU. Install Go from https://go.dev/dl/ and re-run." >&2; exit 1 ;;
  esac
  command -v curl >/dev/null 2>&1 || { echo "curl is required to download Go." >&2; exit 1; }

  version=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)
  echo "Installing $version to \$HOME/.local/go ..."
  mkdir -p "$HOME/.local"
  rm -rf "$HOME/.local/go"
  curl -fsSL "https://go.dev/dl/${version}.${os}-${arch}.tar.gz" | tar -C "$HOME/.local" -xz
}

# Ports below 1024 are privileged on Linux. Give the built binary the capability instead of running as root.
allow_low_ports() {
  local start
  [ "$(uname -s)" = Linux ] || return 0
  [ "$(id -u)" -eq 0 ] && return 0
  start=$(cat /proc/sys/net/ipv4/ip_unprivileged_port_start 2>/dev/null || echo 1024)
  [ "$start" -le 443 ] && return 0

  if command -v setcap >/dev/null 2>&1 && command -v sudo >/dev/null 2>&1; then
    echo "Allowing $BIN to bind port 443 (needs sudo) ..."
    sudo setcap 'cap_net_bind_service=+ep' "$BIN" && return 0
  fi
  echo "warning: could not grant permission to bind port 443. Run this script as root, or run:" >&2
  echo "  sudo setcap 'cap_net_bind_service=+ep' $BIN" >&2
}

if env_is_complete; then
  read -rp "Found a complete $ENV_FILE. Use it? [Y/n]: " reuse
  case ${reuse:-y} in
    [Nn]*) configure_env ;;
  esac
else
  configure_env
fi

export PATH="$HOME/.local/go/bin:/usr/local/go/bin:$PATH"
if ! go_is_usable; then
  install_go
  go_is_usable || { echo "Go installation failed." >&2; exit 1; }
fi

echo "Building ..."
go build -ldflags="-s -w" -o "$BIN" ./cmd/server
allow_low_ports

echo
echo "Make sure TCP ports 443 (web visitors) and 4443 (tunnel clients) are open in your firewall."
echo
exec "./$BIN"
