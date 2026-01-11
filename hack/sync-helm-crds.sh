#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

CRD_SOURCE="${PROJECT_ROOT}/config/crd/bases"
CRD_DEST="${PROJECT_ROOT}/charts/slingshot-operator/crds"

if [[ ! -d "${CRD_SOURCE}" ]]; then
    echo "Error: CRD source directory not found: ${CRD_SOURCE}" >&2
    echo "Run 'make manifests' first." >&2
    exit 1
fi

mkdir -p "${CRD_DEST}"
cp "${CRD_SOURCE}"/*.yaml "${CRD_DEST}/"

echo "Synced CRDs from ${CRD_SOURCE} to ${CRD_DEST}"
ls -1 "${CRD_DEST}"
