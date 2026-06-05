#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

COMPOSE="docker compose"

color() { printf "\033[%sm%s\033[0m\n" "$1" "$2"; }
info()  { color "1;34" "==> $*"; }
ok()    { color "1;32" "OK  $*"; }
warn()  { color "1;33" "!!  $*"; }
err()   { color "1;31" "ERR $*" >&2; }

require_engine() {
  if ! command -v docker >/dev/null 2>&1; then
    err "docker not found in PATH. Install Docker (or adapt COMPOSE for Podman)."
    exit 1
  fi
  if ! docker info >/dev/null 2>&1; then
    err "Docker daemon is not running. Start Docker Desktop / the docker service and retry."
    exit 1
  fi
  ok "Docker daemon is running."
}

cmd_up() {
  require_engine
  info "Bringing the stack up (build + detached)..."
  $COMPOSE up -d --build
  echo
  info "Container status:"
  $COMPOSE ps
  echo
  ok "Stack is up."
  echo "    App (via Nginx LB):  http://localhost:8080/accounts/1/balance"
  echo "    Grafana:             http://localhost:3000   (admin / admin)"
  echo "    (Postgres & Prometheus are internal-only — no host ports.)"
}

cmd_down() {
  require_engine
  info "Stopping the stack..."
  $COMPOSE down
  ok "Stack stopped. Postgres data persists in ./data/postgres."
}

cmd_reset() {
  require_engine
  local force="${1:-}"
  if [ "$force" != "-y" ] && [ "$force" != "--yes" ]; then
    warn "This DELETES ALL DATA: containers, named volumes AND ./data/postgres."
    printf "Type 'y' to continue: "
    read -r ans || ans=""
    [ "$ans" = "y" ] || { info "Aborted."; return 0; }
  fi
  info "Removing containers and named volumes..."
  $COMPOSE down -v
  info "Removing the Postgres bind mount (./data/postgres)..."
  if ! rm -rf ./data/postgres 2>/dev/null; then
    warn "host rm denied (files owned by container uid); removing via container..."
    docker run --rm -v "$SCRIPT_DIR/data:/data" alpine:3 rm -rf /data/postgres
  fi
  ok "Reset complete. The next 'up' starts a fresh DB and re-runs db/init.sql."
}

cmd_status() {
  require_engine
  $COMPOSE ps
}

cmd_logs() {
  require_engine
  $COMPOSE logs -f --tail=100 "$@"
}

usage() {
  cat <<'EOF'
Usage: ./start-infra.sh [command]

Commands:
  up        Validate Docker, build & start the stack detached, show status (default)
  down      Stop the stack (Postgres data is preserved in ./data/postgres)
  reset     Stop and DELETE everything: volumes + ./data/postgres (fresh DB next up).
            Skips the confirmation prompt with: reset -y
  status    Show container status
  logs      Follow logs (optionally a service: ./start-infra.sh logs app1)
EOF
}

main() {
  local cmd="${1:-up}"
  case "$cmd" in
    up)             cmd_up ;;
    down)           cmd_down ;;
    reset)          shift || true; cmd_reset "$@" ;;
    status)         cmd_status ;;
    logs)           shift || true; cmd_logs "$@" ;;
    -h|--help|help) usage ;;
    *)              err "Unknown command: $cmd"; echo; usage; exit 1 ;;
  esac
}

main "$@"
