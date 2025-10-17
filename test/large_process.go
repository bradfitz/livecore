package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

func main() {
	targetGB := flag.Int("size", 10, "Size of memory to allocate in GB")
	flag.Parse()

	fmt.Printf("Large Process Test Program\n")
	fmt.Printf("PID: %d\n", os.Getpid())
	fmt.Printf("Allocating %d GB of memory...\n", *targetGB)
	fmt.Println()

	// Allocate in chunks to avoid OOM
	chunkSize := 1024 * 1024 * 1024 // 1GB chunks
	chunks := make([][]byte, *targetGB)

	for i := 0; i < *targetGB; i++ {
		start := time.Now()
		chunks[i] = make([]byte, chunkSize)
		// Touch the memory to ensure it's actually allocated
		for j := 0; j < len(chunks[i]); j += 4096 {
			chunks[i][j] = byte(i)
		}
		elapsed := time.Since(start)
		fmt.Printf("Allocated chunk %d/%d (%d GB total) in %v\n",
			i+1, *targetGB, i+1, elapsed)
	}

	fmt.Println()
	fmt.Println("Memory allocated successfully.")
	printMemoryStats()

	// Keep writing to memory to generate dirty pages
	fmt.Println()
	fmt.Println("Starting memory activity (writing to random pages)...")
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			for i := range chunks {
				// Write to a random page in each chunk
				offset := time.Now().UnixNano() % int64(len(chunks[i]))
				chunks[i][offset] = byte(i)
			}
		}
	}()

	fmt.Println()
	fmt.Println("Process ready for core dump.")
	fmt.Println("Run: livecore", os.Getpid(), "output.core")
	fmt.Println()
	fmt.Println("Press Ctrl+C to exit")

	// Keep running
	select {}
}

func printMemoryStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("Memory Stats:\n")
	fmt.Printf("  Alloc:      %d MB\n", m.Alloc/1024/1024)
	fmt.Printf("  TotalAlloc: %d MB\n", m.TotalAlloc/1024/1024)
	fmt.Printf("  Sys:        %d MB\n", m.Sys/1024/1024)
	fmt.Printf("  NumGC:      %d\n", m.NumGC)
}
