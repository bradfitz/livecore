package copy

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"time"
	"unsafe"

	"github.com/bradfitz/livecore/internal/buffer"
	"golang.org/x/sys/unix"
)

// PreCopyEngine handles iterative pre-copy with soft-dirty tracking
type PreCopyEngine struct {
	pid            int
	maxPasses      int
	dirtyThreshold float64
	pageMap        *PageMap
	workerPool     *WorkerPool
	bufferManager  *buffer.Manager
	verbose        bool
}

// NewPreCopyEngine creates a new pre-copy engine
func NewPreCopyEngine(pid int, maxPasses int, dirtyThreshold float64, workers int, bufferManager *buffer.Manager, verbose bool) *PreCopyEngine {
	return &PreCopyEngine{
		pid:            pid,
		maxPasses:      maxPasses,
		dirtyThreshold: dirtyThreshold,
		pageMap:        NewPageMap(pid),
		workerPool:     NewWorkerPool(workers),
		bufferManager:  bufferManager,
		verbose:        verbose,
	}
}

// PageMap represents the soft-dirty view of pages (imported from proc package)
type PageMap struct {
	pid      int
	pageSize int
}

// NewPageMap creates a new PageMap for the given process
func NewPageMap(pid int) *PageMap {
	return &PageMap{
		pid:      pid,
		pageSize: GetPageSize(),
	}
}

// ClearSoftDirty clears the soft-dirty bits for the process
func (pm *PageMap) ClearSoftDirty() error {
	clearRefsPath := fmt.Sprintf("/proc/%d/clear_refs", pm.pid)
	file, err := os.OpenFile(clearRefsPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open clear_refs: %w", err)
	}
	defer file.Close()

	// Write "4" to clear soft-dirty bits
	if _, err := file.WriteString("4\n"); err != nil {
		return fmt.Errorf("failed to clear soft-dirty bits: %w", err)
	}

	return nil
}

// GetDirtyPages reads the pagemap to find dirty pages
func (pm *PageMap) GetDirtyPages(vmas []VMA) (map[uintptr]bool, error) {
	dirtyPages := make(map[uintptr]bool)

	// Create a reusable buffer for pagemap reads
	var buffer bytes.Buffer

	for _, vma := range vmas {
		if err := pm.scanVMAForDirtyPages(vma, dirtyPages, &buffer); err != nil {
			return nil, fmt.Errorf("failed to scan VMA %x-%x: %w", vma.Start, vma.End, err)
		}
	}

	return dirtyPages, nil
}

// scanVMAForDirtyPages scans a VMA for dirty pages using a reusable buffer
func (pm *PageMap) scanVMAForDirtyPages(vma VMA, dirtyPages map[uintptr]bool, buffer *bytes.Buffer) error {
	pagemapPath := fmt.Sprintf("/proc/%d/pagemap", pm.pid)
	file, err := os.Open(pagemapPath)
	if err != nil {
		return fmt.Errorf("failed to open pagemap: %w", err)
	}
	defer file.Close()

	// Calculate page-aligned start and end
	start := vma.Start &^ uintptr(pm.pageSize-1)
	end := (vma.End + uintptr(pm.pageSize-1)) &^ uintptr(pm.pageSize-1)

	// Calculate the number of pages in this VMA
	numPages := int((end - start) / uintptr(pm.pageSize))
	if numPages == 0 {
		return nil
	}

	// Calculate the total bytes needed for all pagemap entries
	// Each pagemap entry is 8 bytes
	totalBytes := numPages * 8

	// Reset buffer for reuse
	buffer.Reset()

	// Ensure buffer has enough capacity
	if buffer.Cap() < totalBytes {
		buffer.Grow(totalBytes - buffer.Cap())
	}

	// Calculate the starting offset in the pagemap file
	startOffset := int64(start / uintptr(pm.pageSize) * 8)

	// Read all pagemap entries for this VMA in one system call
	// Get available buffer space and ensure it's large enough
	available := buffer.AvailableBuffer()
	if len(available) < totalBytes {
		// If available buffer is too small, we need to grow more
		buffer.Grow(totalBytes - len(available))
		available = buffer.AvailableBuffer()
	}
	readBuffer := available[:totalBytes]
	n, err := file.ReadAt(readBuffer, startOffset)
	if err != nil {
		// Skip VMAs that can't be read (like vsyscall, etc.)
		if err == os.ErrNotExist || n == 0 {
			return nil
		}
		return fmt.Errorf("failed to read pagemap entries: %w", err)
	}

	// Process each pagemap entry from the buffer
	for i := 0; i < numPages; i++ {
		addr := start + uintptr(i*pm.pageSize)
		entryOffset := i * 8

		// Check if we have enough data for this entry
		if entryOffset+8 > len(readBuffer) {
			break
		}

		// Parse the entry from the buffer
		entry := readBuffer[entryOffset : entryOffset+8]
		entryValue := uint64(entry[0]) | uint64(entry[1])<<8 | uint64(entry[2])<<16 | uint64(entry[3])<<24 |
			uint64(entry[4])<<32 | uint64(entry[5])<<40 | uint64(entry[6])<<48 | uint64(entry[7])<<56

		// Bit 55 is the soft-dirty bit
		softDirty := (entryValue & (1 << 55)) != 0

		if softDirty {
			dirtyPages[addr] = true
		}
	}

	return nil
}

// CalculateDirtyRatio calculates the ratio of dirty pages
func (pm *PageMap) CalculateDirtyRatio(vmas []VMA) (float64, error) {
	dirtyPages, err := pm.GetDirtyPages(vmas)
	if err != nil {
		return 0, fmt.Errorf("failed to get dirty pages: %w", err)
	}

	totalPages := 0
	for _, vma := range vmas {
		pages := int((vma.End - vma.Start + uintptr(pm.pageSize-1)) / uintptr(pm.pageSize))
		totalPages += pages
	}

	if totalPages == 0 {
		return 0, nil
	}

	dirtyCount := len(dirtyPages)
	return float64(dirtyCount) / float64(totalPages), nil
}

// VMA represents a virtual memory area
type VMA struct {
	Start uintptr
	End   uintptr
	Size  uint64
	Perms Perm
	// Add other fields as needed
}

// Perm represents memory permissions
type Perm uint8

const (
	PermRead  Perm = 1 << 0
	PermWrite Perm = 1 << 1
	PermExec  Perm = 1 << 2
)

// PreCopyResult contains the result of pre-copy
type PreCopyResult struct {
	Passes          int
	TotalTime       time.Duration
	FinalDirtyRatio float64
	VMAs            []VMA
	DirtyPages      map[uintptr]bool
}

// RunPreCopy runs the iterative pre-copy process
func (pce *PreCopyEngine) RunPreCopy(vmas []VMA) (*PreCopyResult, error) {
	if pce.verbose {
		fmt.Printf("Starting pre-copy for %d VMAs\n", len(vmas))
	}

	startTime := time.Now()

	// Start worker pool
	pce.workerPool.Start()
	defer pce.workerPool.Stop()

	// Clear soft-dirty bits
	if err := pce.pageMap.ClearSoftDirty(); err != nil {
		return nil, fmt.Errorf("failed to clear soft-dirty bits: %w", err)
	}

	// Run pre-copy passes
	for pass := 1; pass <= pce.maxPasses; pass++ {
		if pce.verbose {
			fmt.Printf("Pre-copy pass %d/%d\n", pass, pce.maxPasses)
		}

		passStart := time.Now()

		// Copy all pages
		if err := pce.copyAllPages(vmas); err != nil {
			return nil, fmt.Errorf("failed to copy pages in pass %d: %w", pass, err)
		}

		// Check dirty ratio
		dirtyRatio, err := pce.pageMap.CalculateDirtyRatio(vmas)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate dirty ratio: %w", err)
		}

		passTime := time.Since(passStart)
		if pce.verbose {
			fmt.Printf("Pass %d completed in %v, dirty ratio: %.2f%%\n",
				pass, passTime, dirtyRatio*100)
		}

		// Check if we should stop
		if dirtyRatio < pce.dirtyThreshold {
			if pce.verbose {
				fmt.Printf("Dirty ratio %.2f%% below threshold %.2f%%, stopping pre-copy\n",
					dirtyRatio*100, pce.dirtyThreshold*100)
			}
			break
		}

		// Clear soft-dirty bits for next pass
		if pass < pce.maxPasses {
			if err := pce.pageMap.ClearSoftDirty(); err != nil {
				return nil, fmt.Errorf("failed to clear soft-dirty bits: %w", err)
			}
		}
	}

	// Get final dirty pages
	dirtyPages, err := pce.pageMap.GetDirtyPages(vmas)
	if err != nil {
		return nil, fmt.Errorf("failed to get final dirty pages: %w", err)
	}

	finalDirtyRatio, err := pce.pageMap.CalculateDirtyRatio(vmas)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate final dirty ratio: %w", err)
	}

	totalTime := time.Since(startTime)

	if pce.verbose {
		fmt.Printf("Pre-copy completed in %v, final dirty ratio: %.2f%%\n",
			totalTime, finalDirtyRatio*100)
	}

	return &PreCopyResult{
		Passes:          pce.maxPasses,
		TotalTime:       totalTime,
		FinalDirtyRatio: finalDirtyRatio,
		VMAs:            vmas,
		DirtyPages:      dirtyPages,
	}, nil
}

// copyAllPages copies all pages in the given VMAs
func (pce *PreCopyEngine) copyAllPages(vmas []VMA) error {
	if pce.verbose {
		fmt.Printf("Copying %d VMAs using process_vm_readv\n", len(vmas))
	}

	// Copy each VMA using process_vm_readv
	for _, vma := range vmas {
		if err := pce.copyVMA(vma); err != nil {
			return fmt.Errorf("failed to copy VMA %x-%x: %w", vma.Start, vma.End, err)
		}
	}

	return nil
}

// copyVMA copies a single VMA
func (pce *PreCopyEngine) copyVMA(vma VMA) error {
	t0 := time.Now()
	if pce.verbose {
		defer func() {
			d := time.Since(t0)
			if d > 10*time.Millisecond {
				log.Printf("copyVMA %x-%x (%d bytes) took %v", vma.Start, vma.End, vma.Size, d)
			}
		}()
	}

	// Calculate page-aligned boundaries
	pageSize := uint64(GetPageSize())
	start := uint64(vma.Start) &^ (pageSize - 1)
	end := (uint64(vma.End) + pageSize - 1) &^ (pageSize - 1)

	// Get the offset for this VMA region in the temp file (once per VMA)
	vmaOffset := pce.bufferManager.GetOffsetForVMA(uint64(vma.Start), uint64(vma.End-vma.Start))

	// Get the mmap pointer for this VMA
	mmapPtr, err := pce.bufferManager.GetMmapPointer(vmaOffset)
	if err != nil {
		return fmt.Errorf("failed to get mmap pointer: %w", err)
	}

	// Copy the entire VMA in one ProcessVMReadv call
	vmaSize := end - start
	err = CopyMemoryToMmap(pce.pid, uintptr(start), vmaSize, mmapPtr)
	if err != nil {
		// Skip pages that can't be read (like vsyscall, etc.)
		if err == unix.ENOENT || err == unix.EFAULT {
			// Fill with zeros for unreadable pages
			if pce.verbose {
				log.Printf("Skipping unreadable VMA %x-%x: %v", vma.Start, vma.End, err)
			}
			return nil
		}
		return fmt.Errorf("failed to read VMA %x-%x: %w", vma.Start, vma.End, err)
	}

	// No sync needed - we're reading from mmap memory directly

	return nil
}

// GetPageSize returns the system page size
func GetPageSize() int {
	// This would return the actual page size
	return 4096
}

// CopyMemoryToMmap copies memory from a process to mmap using ProcessVMReadv
func CopyMemoryToMmap(pid int, srcAddr uintptr, size uint64, mmapPtr unsafe.Pointer) error {
	localIovec := unix.Iovec{
		Base: (*byte)(mmapPtr),
		Len:  size,
	}
	remoteIovec := unix.RemoteIovec{
		Base: srcAddr,
		Len:  int(size),
	}

	_, err := unix.ProcessVMReadv(pid, []unix.Iovec{localIovec}, []unix.RemoteIovec{remoteIovec}, 0)
	if err != nil {
		if err == unix.ENOENT || err == unix.EFAULT {
			return err // Let caller decide how to handle unreadable memory
		}
		return fmt.Errorf("failed to read memory at %x: %w", srcAddr, err)
	}
	return nil
}

// AlignToPage aligns a value to page boundary
func AlignToPage(size uint64) uint64 {
	pageSize := uint64(GetPageSize())
	return (size + pageSize - 1) &^ (pageSize - 1)
}
