#!/bin/bash

set -e

echo "Testing livecore..."

# Build the test program
echo "Building test program..."
go build -o testprog testprog.go

# Start the test program in the background
echo "Starting test program..."
./testprog &
TEST_PID=$!

# Wait a bit for the program to start
sleep 2

echo "Test program PID: $TEST_PID"

# Test livecore with different options
echo "Testing basic core dump..."
./livecore -verbose $TEST_PID test_basic.core

echo "Testing with pre-copy..."
./livecore -verbose -passes 2 -dirty-thresh 10 $TEST_PID test_precopy.core

echo "Testing with minimal notes..."
./livecore -verbose -notes minimal $TEST_PID test_minimal.core

# Validate the core files
echo "Validating core files..."

for core in test_basic.core test_precopy.core test_minimal.core; do
    if [ -f "$core" ]; then
        echo "Validating $core..."
        
        # Check if it's a valid ELF file
        file "$core"
        
        # Check ELF header
        readelf -h "$core"
        
        # Check program headers
        readelf -l "$core"
        
        # Check notes
        readelf -n "$core"
        
        echo "Core file $core is valid"
    else
        echo "Core file $core not found!"
        exit 1
    fi
done

# Clean up
echo "Cleaning up..."
kill $TEST_PID 2>/dev/null || true
rm -f testprog test_*.core

echo "All tests passed!"
