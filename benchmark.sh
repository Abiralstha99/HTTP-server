#!/bin/bash
# Usage: ./benchmark.sh SERVER_PID SERVER_LOG [wrk arguments...]
set -euo pipefail

if [ "$#" -lt 3 ]; then
    echo "Usage: $0 SERVER_PID SERVER_LOG [wrk arguments...]" >&2
    exit 2
fi
server_pid=$1
server_log=$2
shift 2

snapshot() {
    local label=$1 previous current line
    previous=$(grep -c '^Counters snapshot:' "$server_log" || true)
    kill -USR1 "$server_pid"
    for ((attempt=0; attempt<100; attempt++)); do
        current=$(grep -c '^Counters snapshot:' "$server_log" || true)
        if [ "$current" -gt "$previous" ]; then
            line=$(grep '^Counters snapshot:' "$server_log" | tail -n 1)
            printf '%s: %s\n' "$label" "$line"
            return 0
        fi
        kill -0 "$server_pid" 2>/dev/null || break
        sleep 0.1
    done
    echo "Failed to receive $label snapshot from server" >&2
    return 1
}

snapshot before
status=0
wrk "$@" || status=$?
snapshot after
exit "$status"
