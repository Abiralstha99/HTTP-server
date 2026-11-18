#!/bin/bash
# Usage: ./benchmark.sh [wrk arguments...]
set -euo pipefail

if [ "$#" -eq 0 ]; then
    echo "Usage: $0 [wrk arguments...]" >&2
    exit 2
fi

exec wrk "$@"
