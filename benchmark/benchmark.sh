#!/bin/bash

set -e

echo "Running livecore pause time benchmark..."

# Build the pause test program
echo "Building pause test program..."
go build -o pause_test pause_test.go

# Start the pause test program in the background
echo "Starting pause test program..."
./pause_test &
TEST_PID=$!

# Wait for the program to start
sleep 2

echo "Pause test program PID: $TEST_PID"

# Run livecore and measure pause time
echo "Running livecore with pause measurement..."
start_time=$(date +%s.%N)
./livecore -verbose -passes 2 -dirty-thresh 5 $TEST_PID benchmark.core
end_time=$(date +%s.%N)

pause_time=$(echo "$end_time - $start_time" | bc)
echo "Total livecore execution time: ${pause_time}s"

# Validate the core file
if [ -f "benchmark.core" ]; then
    echo "Validating core file..."
    file benchmark.core
    readelf -h benchmark.core
    echo "Core file is valid"
else
    echo "Core file not created!"
    exit 1
fi

# Clean up
echo "Cleaning up..."
kill $TEST_PID 2>/dev/null || true
rm -f pause_test benchmark.core

echo "Benchmark completed!"
