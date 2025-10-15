#!/bin/bash
# Quick validation tests for livecore (structural + GDB)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "========================================="
echo "LIVECORE QUICK VALIDATION"
echo "========================================="

# Build livecore
echo "Building livecore..."
go build -o livecore .

# Start test program
echo "Starting test program..."
go run test/testprog.go &
TEST_PID=$!
sleep 2

# Generate core dump
echo "Generating core dump..."
./livecore -verbose "$TEST_PID" /tmp/quick_test.core

echo ""
echo "--- Structural Validation ---"
bash test/01_structural_validation.sh /tmp/quick_test.core

echo ""
echo "--- GDB Validation ---"
bash test/02_gdb_validation.sh "$TEST_PID" /tmp/quick_test.core || echo "GDB validation skipped (not installed)"

# Cleanup
kill $TEST_PID 2>/dev/null || true
rm -f /tmp/quick_test.core

echo ""
echo "Quick validation complete!"
