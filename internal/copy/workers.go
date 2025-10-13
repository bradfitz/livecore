package copy

import (
	"fmt"
	"runtime"
	"sync"

	"golang.org/x/sys/unix"
)

// WorkerPool manages concurrent memory reading workers
type WorkerPool struct {
	workers int
	jobs    chan Job
	results chan Result
	wg      sync.WaitGroup
}

// Job represents a memory reading job
type Job struct {
	VMA    VMA
	Offset uint64
	Size   uint64
	Data   []byte
}

// Result represents the result of a memory reading job
type Result struct {
	Job   Job
	Error error
}

// NewWorkerPool creates a new worker pool
func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	return &WorkerPool{
		workers: workers,
		jobs:    make(chan Job, workers*2),
		results: make(chan Result, workers*2),
	}
}

// Start starts the worker pool
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// Stop stops the worker pool
func (wp *WorkerPool) Stop() {
	close(wp.jobs)
	wp.wg.Wait()
	close(wp.results)
}

// Submit submits a job to the worker pool
func (wp *WorkerPool) Submit(job Job) {
	wp.jobs <- job
}

// GetResult gets a result from the worker pool
func (wp *WorkerPool) GetResult() Result {
	return <-wp.results
}

// worker is the main worker function
func (wp *WorkerPool) worker() {
	defer wp.wg.Done()

	for job := range wp.jobs {
		result := wp.processJob(job)
		wp.results <- result
	}
}

// processJob processes a single job
func (wp *WorkerPool) processJob(job Job) Result {
	// Use process_vm_readv to read memory
	data, err := wp.readMemory(job.VMA.Start, job.Size)
	if err != nil {
		return Result{Job: job, Error: err}
	}

	job.Data = data
	return Result{Job: job, Error: nil}
}

// readMemory reads memory from the target process
func (wp *WorkerPool) readMemory(addr uintptr, size uint64) ([]byte, error) {
	// This is a placeholder implementation
	// In a real implementation, this would use process_vm_readv
	// For now, return zero-filled data
	data := make([]byte, size)
	return data, nil
}

// ProcessVMReadv reads memory from a process using process_vm_readv
func ProcessVMReadv(pid int, localAddr uintptr, remoteAddr uintptr, size uint64) ([]byte, error) {
	// Allocate local buffer
	data := make([]byte, size)

	// Set up iovec for local buffer
	localIovec := unix.Iovec{
		Base: &data[0],
		Len:  size,
	}

	// Set up iovec for remote buffer
	remoteIovec := unix.RemoteIovec{
		Base: remoteAddr,
		Len:  int(size),
	}

	// Call process_vm_readv using unix package
	n, err := unix.ProcessVMReadv(pid, []unix.Iovec{localIovec}, []unix.RemoteIovec{remoteIovec}, 0)
	if err != nil {
		return nil, fmt.Errorf("process_vm_readv failed: %w", err)
	}

	if uint64(n) != size {
		return nil, fmt.Errorf("process_vm_readv read %d bytes, expected %d", n, size)
	}

	return data, nil
}

// ProcessVMReadvBatch reads multiple memory regions in a single call
func ProcessVMReadvBatch(pid int, regions []MemoryRegion) ([]byte, error) {
	if len(regions) == 0 {
		return nil, nil
	}

	// Calculate total size
	totalSize := uint64(0)
	for _, region := range regions {
		totalSize += region.Size
	}

	// Allocate buffer
	data := make([]byte, totalSize)

	// Set up local iovecs
	localIovecs := make([]unix.Iovec, len(regions))
	offset := uint64(0)
	for i, region := range regions {
		localIovecs[i] = unix.Iovec{
			Base: &data[offset],
			Len:  region.Size,
		}
		offset += region.Size
	}

	// Set up remote iovecs
	remoteIovecs := make([]unix.RemoteIovec, len(regions))
	for i, region := range regions {
		remoteIovecs[i] = unix.RemoteIovec{
			Base: region.Addr,
			Len:  int(region.Size),
		}
	}

	// Call process_vm_readv
	n, err := unix.ProcessVMReadv(pid, localIovecs, remoteIovecs, 0)
	if err != nil {
		return nil, fmt.Errorf("process_vm_readv batch failed: %w", err)
	}

	if uint64(n) != totalSize {
		return nil, fmt.Errorf("process_vm_readv batch read %d bytes, expected %d", n, totalSize)
	}

	return data, nil
}

// MemoryRegion represents a memory region to read
type MemoryRegion struct {
	Addr uintptr
	Size uint64
}
