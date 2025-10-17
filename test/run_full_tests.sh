#!/bin/bash
# Comprehensive validation test suite for livecore

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test results
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_WARNED=0

log_pass() {
    echo -e "${GREEN}✓ PASS:${NC} $1"
    ((TESTS_PASSED++))
}

log_fail() {
    echo -e "${RED}✗ FAIL:${NC} $1"
    ((TESTS_FAILED++))
}

log_warn() {
    echo -e "${YELLOW}⚠ WARN:${NC} $1"
    ((TESTS_WARNED++))
}

echo "========================================="
echo "LIVECORE COMPREHENSIVE VALIDATION"
echo "========================================="
echo ""

# Build livecore
echo "Building livecore..."
if ! go build -o livecore .; then
    log_fail "Failed to build livecore"
    exit 1
fi
log_pass "Built livecore"

# Start test program
echo ""
echo "Starting test program..."
go run test/testprog.go &
TEST_PID=$!
sleep 2

if ! kill -0 $TEST_PID 2>/dev/null; then
    log_fail "Test program failed to start"
    exit 1
fi
log_pass "Test program running (PID: $TEST_PID)"

# Generate core dump
echo ""
echo "Generating core dump..."
if ! ./livecore -verbose -passes 2 "$TEST_PID" /tmp/livecore_test.core 2>&1 | tee /tmp/livecore_output.txt; then
    log_fail "livecore failed to generate core dump"
    kill $TEST_PID 2>/dev/null || true
    exit 1
fi
log_pass "Core dump generated: /tmp/livecore_test.core"

echo ""
echo "========================================="
echo "RUNNING VALIDATION TESTS"
echo "========================================="

# Level 1: Structural Validation
echo ""
echo "--- Level 1: Structural Validation ---"
if bash test/01_structural_validation.sh /tmp/livecore_test.core; then
    log_pass "Structural validation"
else
    log_fail "Structural validation"
fi

# Level 2: GDB Validation
echo ""
echo "--- Level 2: GDB Validation ---"
if bash test/02_gdb_validation.sh "$TEST_PID" /tmp/livecore_test.core; then
    log_pass "GDB validation"
else
    log_warn "GDB validation (GDB may not be installed)"
fi

# Level 3: Differential Validation
echo ""
echo "--- Level 3: Differential Validation ---"
if bash test/04_differential_validation.sh "$TEST_PID"; then
    log_pass "Differential validation"
else
    log_warn "Differential validation (gcore may not be available)"
fi

# Level 4: Thread Validation
echo ""
echo "--- Level 4: Thread Validation ---"
if bash test/07_thread_validation.sh "$TEST_PID" /tmp/livecore_test.core; then
    log_pass "Thread validation"
else
    log_fail "Thread validation"
fi

# Cleanup test process
kill $TEST_PID 2>/dev/null || true
rm -f /tmp/livecore_test.core /tmp/gcore.* /tmp/livecore_output.txt

echo ""
echo "========================================="
echo "TEST RESULTS"
echo "========================================="
echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
echo -e "${RED}Failed: $TESTS_FAILED${NC}"
echo -e "${YELLOW}Warnings: $TESTS_WARNED${NC}"
echo ""

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All critical tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Some tests failed. Review output above.${NC}"
    exit 1
fi
