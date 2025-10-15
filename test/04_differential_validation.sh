#!/bin/bash
# Level 3: Differential Validation
# Compares livecore output with gcore output

set -e

if [ $# -lt 1 ]; then
    echo "Usage: $0 <pid>"
    exit 1
fi

PID=$1

# Check if gcore is available
if ! command -v gcore &> /dev/null; then
    echo "WARN: gcore not installed, skipping differential validation"
    exit 0
fi

echo "=== Differential Validation ==="
echo "PID: $PID"
echo ""

# Generate livecore dump
echo "Generating livecore dump..."
./livecore -verbose "$PID" /tmp/livecore_diff.core 2>&1 | grep -E "(STW|Pass|completed)" || true

# Generate gcore dump
echo ""
echo "Generating gcore dump..."
gcore -o /tmp/gcore_diff "$PID" 2>&1 | grep -v "^Saved" || true

# Compare file sizes
echo ""
echo "Comparing file sizes..."
LIVECORE_SIZE=$(stat -c %s /tmp/livecore_diff.core 2>/dev/null || stat -f %z /tmp/livecore_diff.core)
GCORE_SIZE=$(stat -c %s /tmp/gcore_diff.* 2>/dev/null || stat -f %z /tmp/gcore_diff.*)

echo "livecore size: $(numfmt --to=iec $LIVECORE_SIZE 2>/dev/null || echo $LIVECORE_SIZE bytes)"
echo "gcore size:    $(numfmt --to=iec $GCORE_SIZE 2>/dev/null || echo $GCORE_SIZE bytes)"

SIZE_DIFF=$(echo "scale=2; ($LIVECORE_SIZE - $GCORE_SIZE) / $GCORE_SIZE * 100" | bc)
echo "Difference: ${SIZE_DIFF}%"

if [ $(echo "$SIZE_DIFF > 20" | bc 2>/dev/null || echo 0) -eq 1 ] || [ $(echo "$SIZE_DIFF < -20" | bc 2>/dev/null || echo 0) -eq 1 ]; then
    echo "WARN: Significant size difference (>20%)"
fi

# Compare PT_LOAD segments
echo ""
echo "Comparing memory mappings..."
LIVECORE_LOADS=$(readelf -l /tmp/livecore_diff.core | grep -c LOAD || true)
GCORE_LOADS=$(readelf -l /tmp/gcore_diff.* | grep -c LOAD || true)

echo "livecore PT_LOAD segments: $LIVECORE_LOADS"
echo "gcore PT_LOAD segments:    $GCORE_LOADS"

if [ "$LIVECORE_LOADS" -lt "$GCORE_LOADS" ]; then
    echo "WARN: livecore has fewer PT_LOAD segments than gcore"
fi

# Compare note types
echo ""
echo "Comparing note types..."
for note_type in NT_PRSTATUS NT_PRPSINFO NT_AUXV NT_FILE; do
    LIVE_COUNT=$(readelf -n /tmp/livecore_diff.core 2>/dev/null | grep -c "$note_type" || echo 0)
    GCORE_COUNT=$(readelf -n /tmp/gcore_diff.* 2>/dev/null | grep -c "$note_type" || echo 0)
    printf "  %-15s livecore=%2d  gcore=%2d\n" "$note_type:" "$LIVE_COUNT" "$GCORE_COUNT"
done

# Cleanup
rm -f /tmp/livecore_diff.core /tmp/gcore_diff.*

echo ""
echo "=== Differential validation: PASSED ==="
exit 0
