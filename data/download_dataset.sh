#!/usr/bin/env bash
# Downloads the SNAP email-Enron dataset used for this benchmark.
#
# Dataset: email-Enron (Stanford Large Network Dataset Collection)
# Source:  https://snap.stanford.edu/data/email-Enron.html
# Paper:   J. Leskovec, J. Kleinberg, C. Faloutsos. "Graphs over Time:
#          Densification Laws, Shrinking Diameters and Possible Explanations."
#          KDD 2005.
# Size:    36,692 nodes, 183,831 directed edges (email communication).
#          Comfortably inside the assignment's 100k-500k relationship band
#          and small enough to fit every platform's free/entry tier.
#
# Run this from the repo root: bash data/download_dataset.sh

set -euo pipefail

DATA_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
URL="https://snap.stanford.edu/data/email-Enron.txt.gz"
DEST_GZ="${DATA_DIR}/email-Enron.txt.gz"
DEST_TXT="${DATA_DIR}/email-Enron.txt"

echo "Downloading ${URL} ..."
curl -fL --retry 3 -o "${DEST_GZ}" "${URL}"

echo "Decompressing ..."
gunzip -kf "${DEST_GZ}"

LINE_COUNT=$(grep -vc '^#' "${DEST_TXT}")
echo "Done. ${DEST_TXT} has ${LINE_COUNT} edge lines (excluding comment header)."
echo "Next: go run ./cmd/prepare-dataset"
