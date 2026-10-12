#!/bin/sh
# Waits for the frontend and registers the SEMAR namespace if missing.
set -eu
NS="${SEMAR_TEMPORAL_NAMESPACE:-semar}"
until temporal operator cluster health --address "$TEMPORAL_ADDRESS" >/dev/null 2>&1; do
  echo "waiting for temporal frontend"; sleep 2
done
if temporal operator namespace describe --namespace "$NS" --address "$TEMPORAL_ADDRESS" >/dev/null 2>&1; then
  echo "namespace $NS exists"
else
  temporal operator namespace create --namespace "$NS" --retention 72h --address "$TEMPORAL_ADDRESS"
fi
