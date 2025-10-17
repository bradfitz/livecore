#!/bin/bash
# Level 2: GDB Validation
# Verifies GDB can load and navigate the core dump

set -e

if [ $# -lt 2 ]; then
    echo "Usage: $0 <pid> <core_file>"
    exit 1
fi

PID=$1
CORE_FILE=$2
EXECUTABLE=$(readlink -f /proc/$PID/exe 2>/dev/null || echo "")

if [ -z "$EXECUTABLE" ]; then
    echo "ERROR: Cannot determine executable for PID $PID"
    exit 1
fi

if [ ! -f "$CORE_FILE" ]; then
    echo "ERROR: Core file not found: $CORE_FILE"
    exit 1
fi

# Check if GDB is available
if ! command -v gdb &> /dev/null; then
    echo "WARN: GDB not installed, skipping validation"
    exit 0
fi

echo "=== GDB Validation ==="
echo "Executable: $EXECUTABLE"
echo "Core file: $CORE_FILE"
echo ""

# Create GDB command script
cat > /tmp/gdb_commands.txt <<EOF
set pagination off
set confirm off

# Try to load the core
core-file $CORE_FILE

# Basic sanity checks
echo \\n=== Thread Info ===\\n
info threads

echo \\n=== Registers ===\\n
info registers

echo \\n=== Backtrace ===\\n
backtrace

quit
EOF

echo "Running GDB..."
if gdb -batch -x /tmp/gdb_commands.txt "$EXECUTABLE" > /tmp/gdb_output.txt 2>&1; then
    echo "PASS: GDB loaded core file"
else
    echo "FAIL: GDB failed to load core"
    cat /tmp/gdb_output.txt
    rm -f /tmp/gdb_commands.txt /tmp/gdb_output.txt
    exit 1
fi

# Check output for issues
if grep -q "No core file" /tmp/gdb_output.txt; then
    echo "FAIL: GDB reports no core file"
    cat /tmp/gdb_output.txt
    rm -f /tmp/gdb_commands.txt /tmp/gdb_output.txt
    exit 1
fi

if grep -q "warning" /tmp/gdb_output.txt; then
    echo "WARN: GDB reported warnings"
    grep "warning" /tmp/gdb_output.txt || true
fi

if grep -q "Thread" /tmp/gdb_output.txt; then
    echo "PASS: Thread information present"
else
    echo "WARN: No thread information found"
fi

rm -f /tmp/gdb_commands.txt /tmp/gdb_output.txt

echo ""
echo "=== GDB validation: PASSED ==="
exit 0
