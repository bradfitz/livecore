package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// Global memory pool to keep memory allocated
var memoryPool [][]byte
var poolMutex sync.RWMutex

func main() {
	port := "8080"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	fmt.Printf("HTTP Server PID: %d\n", os.Getpid())
	fmt.Printf("Starting HTTP server on port %s\n", port)

	// Start memory waster goroutine early
	go wasteMemory()

	// Allocate some memory to make the core dump interesting
	allocateMemory()

	// Set up HTTP handlers
	http.HandleFunc("/", rootHandler)
	http.HandleFunc("/memory", memoryHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/gc", gcHandler)

	// Start a background goroutine that continuously allocates memory
	go memoryAllocator()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)

	// Start the server
	server := &http.Server{
		Addr:         "127.0.0.1:" + port,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		fmt.Println("HTTP server is running...")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start:", err)
		}
	}()

	// Wait for shutdown signal
	<-sigChan
	fmt.Println("Shutdown signal received, shutting down server...")

	// Give the server a moment to finish current requests
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %v", err)
	}

	fmt.Println("Server shutdown complete")
	os.Exit(0)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello from livecore test server!\n")
	fmt.Fprintf(w, "PID: %d\n", os.Getpid())
	fmt.Fprintf(w, "Goroutines: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "Memory allocated: %d chunks\n", len(memoryPool))
}

func memoryHandler(w http.ResponseWriter, r *http.Request) {
	poolMutex.RLock()
	defer poolMutex.RUnlock()

	fmt.Fprintf(w, "Memory pool status:\n")
	fmt.Fprintf(w, "Chunks: %d\n", len(memoryPool))

	totalSize := 0
	for i, chunk := range memoryPool {
		if i < 10 { // Show first 10 chunks
			fmt.Fprintf(w, "Chunk %d: %d bytes\n", i, len(chunk))
		}
		totalSize += len(chunk)
	}
	fmt.Fprintf(w, "Total size: %d bytes\n", totalSize)
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	fmt.Fprintf(w, "Runtime status:\n")
	fmt.Fprintf(w, "Goroutines: %d\n", runtime.NumGoroutine())
	fmt.Fprintf(w, "Heap alloc: %d bytes\n", m.HeapAlloc)
	fmt.Fprintf(w, "Heap sys: %d bytes\n", m.HeapSys)
	fmt.Fprintf(w, "Stack inuse: %d bytes\n", m.StackInuse)
	fmt.Fprintf(w, "GC cycles: %d\n", m.NumGC)
}

func gcHandler(w http.ResponseWriter, r *http.Request) {
	runtime.GC()
	fmt.Fprintf(w, "Garbage collection triggered\n")
}

func allocateMemory() {
	// Allocate various sizes of memory to create interesting patterns
	sizes := []int{1024, 4096, 16384, 65536, 262144, 1048576} // 1KB to 1MB

	for i := 0; i < 100; i++ {
		size := sizes[i%len(sizes)]
		chunk := make([]byte, size)

		// Fill with some pattern
		for j := range chunk {
			chunk[j] = byte((i + j) % 256)
		}

		poolMutex.Lock()
		memoryPool = append(memoryPool, chunk)
		poolMutex.Unlock()
	}

	fmt.Printf("Allocated %d memory chunks\n", len(memoryPool))
}

func memoryAllocator() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Allocate a new chunk every 5 seconds
		size := 1024 * 1024 // 1MB
		chunk := make([]byte, size)

		// Fill with some data
		for i := range chunk {
			chunk[i] = byte(time.Now().Unix() % 256)
		}

		poolMutex.Lock()
		memoryPool = append(memoryPool, chunk)
		poolMutex.Unlock()

		fmt.Printf("Allocated new chunk, total chunks: %d\n", len(memoryPool))
	}
}

func wasteMemory() {
	var wasted [][]byte
	defer func() {
		fmt.Printf("wasteMemory goroutine allocated %d chunks\n", len(wasted))
	}()

	for range 1000 {
		wasted = append(wasted, make([]byte, 1000))
	}

	// Keep the goroutine alive and prevent GC
	select {}
}
