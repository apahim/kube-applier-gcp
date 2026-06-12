#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-kube-applier-dev}"
MC_NAME="${MANAGEMENT_CLUSTER:-dev-local}"
PROJECT="${FIRESTORE_PROJECT:-test-project}"
EMULATOR="${FIRESTORE_EMULATOR_HOST:-localhost:8219}"
VERBOSITY="${LOG_VERBOSITY:-4}"

echo "Building kube-applier-gcp..."
make build

echo ""
echo "Running kube-applier-gcp against Kind cluster '${CLUSTER_NAME}' + Firestore emulator at ${EMULATOR}"
echo "  Management cluster: ${MC_NAME}"
echo "  Firestore database: mc-${MC_NAME}"
echo ""

export FIRESTORE_EMULATOR_HOST="${EMULATOR}"

exec ./kube-applier-gcp \
  --kubeconfig="${KUBECONFIG:-$HOME/.kube/config}" \
  --namespace=kube-applier-system \
  --management-cluster="${MC_NAME}" \
  --firestore-project="${PROJECT}" \
  --log-verbosity="${VERBOSITY}"
