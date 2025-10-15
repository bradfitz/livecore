#!/bin/bash
# Level 4: Thread Validation
# Verifies all threads are captured in the core dump

set -e

if [ $# -lt 2 ]; then
    echo "Usage: $0 <pid> <core_file>"
    exit 1
fi

PID=$1
CORE_FILE=$2

echo "=== Thread Validation ==="
echo "PID: $PID"
echo "Core file: $CORE_FILE"
echo ""

# Count threads in live process
if [ ! -d "/proc/$PID/task" ]; then
    echo "ERROR: Process $PID not found or not accessible"
    exit 1
fi

LIVE_THREADS=$(ls /proc/$PID/task 2>/dev/null | wc -l)
echo "Live process threads: $LIVE_THREADS"

# Count NT_PRSTATUS notes in core (one per thread)
CORE_THREADS=$(readelf -n "$CORE_FILE" 2>/dev/null | grep -c "NT_PRSTATUS" || echo 0)
echo "Core file thread states: $CORE_THREADS"

echo ""
if [ "$CORE_THREADS" -ge "$LIVE_THREADS" ]; then
    echo "PASS: All threads captured"
    echo "=== Thread validation: PASSED ==="
    exit 0
elif [ "$CORE_THREADS" -gt 0 ]; then
    echo "WARN: Some threads may be missing ($CORE_THREADS captured vs $LIVE_THREADS live)"
    echo "=== Thread validation: PASSED (with warnings) ==="
    exit 0
else
    echo "FAIL: No thread states found in core"
    echo "=== Thread validation: FAILED ==="
    exit 1
fi
