#!/bin/bash
# Level 1: Structural Validation
# Verifies the core file is a structurally valid ELF core dump

set -e

CORE_FILE=$1

if [ -z "$CORE_FILE" ]; then
    echo "Usage: $0 <core_file>"
    exit 1
fi

if [ ! -f "$CORE_FILE" ]; then
    echo "ERROR: Core file not found: $CORE_FILE"
    exit 1
fi

echo "=== Structural Validation ==="
echo "Core file: $CORE_FILE"
echo ""

FAILED=0

# Test 1: Is it a valid ELF file?
echo -n "Test 1: Valid ELF 64-bit file... "
if file "$CORE_FILE" | grep -q "ELF 64-bit"; then
    echo "PASS"
else
    echo "FAIL"
    ((FAILED++))
fi

# Test 2: Is it a core file?
echo -n "Test 2: Identified as core file... "
if file "$CORE_FILE" | grep -q "core file"; then
    echo "PASS"
else
    echo "FAIL"
    ((FAILED++))
fi

# Test 3: Can readelf parse it?
echo -n "Test 3: readelf can parse header... "
if readelf -h "$CORE_FILE" > /dev/null 2>&1; then
    echo "PASS"
else
    echo "FAIL"
    ((FAILED++))
fi

# Test 4: Does it have PT_NOTE segment?
echo -n "Test 4: Has PT_NOTE segment... "
if readelf -l "$CORE_FILE" | grep -q "NOTE"; then
    echo "PASS"
else
    echo "FAIL"
    ((FAILED++))
fi

# Test 5: Does it have PT_LOAD segments?
echo -n "Test 5: Has PT_LOAD segments... "
LOAD_COUNT=$(readelf -l "$CORE_FILE" | grep -c "LOAD" || true)
if [ "$LOAD_COUNT" -gt 0 ]; then
    echo "PASS ($LOAD_COUNT segments)"
else
    echo "FAIL"
    ((FAILED++))
fi

# Test 6: Check for required notes
echo ""
echo "Checking note types..."
readelf -n "$CORE_FILE" > /tmp/notes_check.txt 2>&1

echo -n "  NT_PRSTATUS (thread status)... "
if grep -q "NT_PRSTATUS" /tmp/notes_check.txt; then
    echo "PASS"
else
    echo "WARN (missing)"
    # Not failing, just warning
fi

echo -n "  NT_AUXV (auxiliary vector)... "
if grep -q "NT_AUXV" /tmp/notes_check.txt; then
    echo "PASS"
else
    echo "WARN (missing)"
fi

rm -f /tmp/notes_check.txt

echo ""
if [ $FAILED -eq 0 ]; then
    echo "=== Structural validation: PASSED ==="
    exit 0
else
    echo "=== Structural validation: FAILED ($FAILED tests) ==="
    exit 1
fi
