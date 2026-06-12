#!/usr/bin/env bash
set -euo pipefail

HOST_PORT="${FIRESTORE_EMULATOR_HOST:-localhost:8219}"

cleanup() {
  echo ""
  echo "Stopping Firestore emulator..."
  kill -- -$$ 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "Starting Firestore emulator on ${HOST_PORT}..."
echo ""
echo "In other terminals, export:"
echo "  export FIRESTORE_EMULATOR_HOST=${HOST_PORT}"
echo ""

gcloud emulators firestore start --host-port="${HOST_PORT}" &
wait
