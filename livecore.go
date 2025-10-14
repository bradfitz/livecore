package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/bradfitz/livecore/internal/buffer"
	"github.com/bradfitz/livecore/internal/copy"
	"github.com/bradfitz/livecore/internal/elfcore"
	"github.com/bradfitz/livecore/internal/proc"
	"golang.org/x/sys/unix"
)

// Note: Old temp file management removed - now using BufferManager

// Config holds the configuration for livecore
type Config struct {
	Pid            int
	OutputFile     string
	MaxPasses      int
	DirtyThreshold float64
	Concurrency    int
	Verbose        bool
}

// parseFlags parses command line flags
func parseFlags() (*Config, error) {
	config := &Config{}

	flag.IntVar(&config.MaxPasses, "passes", 2, "maximum pre-copy passes")
	flag.Float64Var(&config.DirtyThreshold, "dirty-thresh", 5.0, "stop when dirty < threshold (percentage)")
	flag.IntVar(&config.Concurrency, "concurrency", runtime.GOMAXPROCS(0), "concurrent read workers")
	flag.BoolVar(&config.Verbose, "verbose", false, "show progress and statistics")

	flag.Parse()

	// Parse positional arguments
	args := flag.Args()
	if len(args) != 2 {
		return nil, fmt.Errorf("usage: livecore [flags] <pid> <output.core>")
	}

	pid, err := strconv.Atoi(args[0])
	if err != nil {
		return nil, fmt.Errorf("invalid PID: %w", err)
	}

	config.Pid = pid
	config.OutputFile = args[1]

	// Validate configuration
	if config.MaxPasses < 1 {
		return nil, fmt.Errorf("max passes must be >= 1")
	}

	if config.DirtyThreshold < 0 || config.DirtyThreshold > 100 {
		return nil, fmt.Errorf("dirty threshold must be between 0 and 100")
	}

	if config.Concurrency < 1 {
		return nil, fmt.Errorf("concurrency must be >= 1")
	}

	// Convert percentage to ratio
	config.DirtyThreshold = config.DirtyThreshold / 100.0

	return config, nil
}

func main() {
	config, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := runLivecore(config); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runLivecore is the main function
func runLivecore(config *Config) error {
	if config.Verbose {
		fmt.Printf("livecore: dumping process %d to %s\n", config.Pid, config.OutputFile)
	}

	startTime := time.Now()

	// Create BufferManager for efficient memory buffering
	bufferManager, err := buffer.NewBufferManager(config.OutputFile)
	if err != nil {
		return fmt.Errorf("failed to create buffer manager: %w", err)
	}
	defer bufferManager.Close()

	// Phase 1: Discovery
	if config.Verbose {
		fmt.Println("Phase 1: Discovery")
	}

	// Parse VMAs
	vmas, err := proc.ParseMaps(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to parse maps: %w", err)
	}

	if config.Verbose {
		fmt.Printf("Found %d VMAs\n", len(vmas))
	}

	// Parse threads
	threads, err := proc.ParseThreads(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to parse threads: %w", err)
	}

	if config.Verbose {
		fmt.Printf("Found %d threads\n", len(threads))
	}

	// Parse auxiliary vector
	_, err = proc.GetAuxv(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to get auxv: %w", err)
	}

	// Phase 2: Pre-copy (if enabled)
	if config.Verbose {
		fmt.Printf("MaxPasses: %d, DirtyThreshold: %.2f\n", config.MaxPasses, config.DirtyThreshold)
	}
	if config.MaxPasses > 0 {
		if config.Verbose {
			fmt.Println("Phase 2: Pre-copy")
		}

		preCopyEngine := copy.NewPreCopyEngine(
			config.Pid,
			config.MaxPasses,
			config.DirtyThreshold,
			config.Concurrency,
			bufferManager,
			config.Verbose,
		)

		// Convert proc.VMA to copy.VMA
		copyVMAs := convertVMAsToCopy(vmas)
		result, err := preCopyEngine.RunPreCopy(copyVMAs)
		if err != nil {
			return fmt.Errorf("pre-copy failed: %w", err)
		}

		if config.Verbose {
			fmt.Printf("Pre-copy completed in %v\n", result.TotalTime)
		}
	}

	// Phase 3: Final stop and delta copy
	if config.Verbose {
		fmt.Println("Phase 3: Final stop and delta copy")
	}

	log.Printf("Starting freeze.")
	stopStart := time.Now()

	// Freeze all threads
	frozenThreads, err := proc.FreezeAllThreads(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to freeze threads: %w", err)
	}

	// Collect register state
	if err := proc.CollectThreadRegisters(frozenThreads); err != nil {
		proc.UnfreezeAllThreads(frozenThreads)
		return fmt.Errorf("failed to collect registers: %w", err)
	}

	// Re-scan maps (authoritative at stop time)
	finalVMAs, err := proc.ParseMaps(config.Pid)
	if err != nil {
		proc.UnfreezeAllThreads(frozenThreads)
		return fmt.Errorf("failed to re-scan maps: %w", err)
	}

	// Copy remaining dirty pages (re-scan after freeze to get current dirty state)
	if err := copyRemainingDirtyPages(config, finalVMAs, bufferManager); err != nil {
		proc.UnfreezeAllThreads(frozenThreads)
		return fmt.Errorf("failed to copy remaining dirty pages: %w", err)
	}

	// Unfreeze threads immediately after final delta copy
	// The core file writing can take a long time, so we don't want to keep
	// the target process frozen during that time
	if err := proc.UnfreezeAllThreads(frozenThreads); err != nil {
		return fmt.Errorf("failed to unfreeze threads: %w", err)
	}

	stopTime := time.Since(stopStart)

	if config.Verbose {
		fmt.Printf("Stop time: %v\n", stopTime)
	}
	// Phase 4: Generate ELF core file
	if config.Verbose {
		fmt.Println("Phase 4: Generate ELF core file")
	}

	// Create core info
	coreInfo := &elfcore.CoreInfo{
		Pid:     config.Pid,
		Threads: convertThreads(frozenThreads),
		VMAs:    convertVMAs(finalVMAs),
	}

	// Create notes
	notes, err := elfcore.CreateCoreNotes(config.Pid, coreInfo.Threads, coreInfo.FileTable)
	if err != nil {
		return fmt.Errorf("failed to create notes: %w", err)
	}

	coreInfo.Notes = notes

	// Write ELF core file
	elfWriter, err := elfcore.NewELFWriter(config.OutputFile, coreInfo, bufferManager)
	if err != nil {
		return fmt.Errorf("failed to create ELF writer: %w", err)
	}
	defer elfWriter.Close()

	if err := elfWriter.WriteCore(); err != nil {
		return fmt.Errorf("failed to write core file: %w", err)
	}

	// Note: No temp files to clean up - using BufferManager

	totalTime := time.Since(startTime)

	if config.Verbose {
		fmt.Printf("Core dump completed in %v (stop time: %v)\n", totalTime, stopTime)
	}

	return nil
}

// copyRemainingDirtyPages copies the remaining dirty pages after freeze
// This is the final delta copy - we only copy pages that are still dirty
// after the process has been frozen, ensuring we capture the final state
func copyRemainingDirtyPages(config *Config, vmas []proc.VMA, bufferManager *buffer.Manager) error {
	if config.Verbose {
		fmt.Println("Copying remaining dirty pages...")
	}

	// Create a new page map to scan for dirty pages after freeze
	pageMap := copy.NewPageMap(config.Pid)

	// Get current dirty pages (after freeze)
	currentDirtyPages, err := pageMap.GetDirtyPages(convertVMAsToCopy(vmas))
	if err != nil {
		return fmt.Errorf("failed to get current dirty pages: %w", err)
	}

	// Copy only the dirty pages using process_vm_readv
	// This is the minimal final copy to capture the exact state at freeze time
	if config.Verbose {
		fmt.Printf("Found %d dirty pages to copy\n", len(currentDirtyPages))
	}
	for pageAddr := range currentDirtyPages {
		if err := copyDirtyPage(config.Pid, pageAddr, bufferManager); err != nil {
			// Log but don't fail - some pages might not be readable
			if config.Verbose {
				fmt.Printf("Warning: failed to copy page at %x: %v\n", pageAddr, err)
			}
		}
	}

	return nil
}

// copyDirtyPage copies a single dirty page to the BufferManager
func copyDirtyPage(pid int, pageAddr uintptr, bufferManager *buffer.Manager) error {
	// Get page size
	pageSize := copy.GetPageSize()

	// Create buffer for the page
	buffer := make([]byte, pageSize)

	// Use process_vm_readv to copy the page
	localIovec := unix.Iovec{
		Base: &buffer[0],
		Len:  uint64(pageSize),
	}
	remoteIovec := unix.RemoteIovec{
		Base: pageAddr,
		Len:  pageSize,
	}

	_, err := unix.ProcessVMReadv(pid, []unix.Iovec{localIovec}, []unix.RemoteIovec{remoteIovec}, 0)
	if err != nil {
		// Skip pages that can't be read (like vsyscall, etc.)
		if err == unix.ENOENT || err == unix.EFAULT {
			return nil
		}
		return fmt.Errorf("failed to read page at %x: %w", pageAddr, err)
	}

	// Write page to BufferManager
	// Get the offset for this page in the temp file
	pageOffset := bufferManager.GetOffsetForVMA(uint64(pageAddr), uint64(pageSize))
	if err := bufferManager.WriteData(pageOffset, buffer); err != nil {
		return fmt.Errorf("failed to write page to buffer manager: %w", err)
	}

	return nil
}

// Note: cleanupTempPageFiles removed - no longer using temp files

// convertThreads converts proc.Thread to elfcore.Thread
func convertThreads(threads []proc.Thread) []elfcore.Thread {
	var result []elfcore.Thread
	for _, thread := range threads {
		result = append(result, elfcore.Thread{
			Tid:       thread.Tid,
			Registers: thread.Registers,
		})
	}
	return result
}

// convertVMAsToCopy converts proc.VMA to copy.VMA
func convertVMAsToCopy(vmas []proc.VMA) []copy.VMA {
	var result []copy.VMA
	for _, vma := range vmas {
		result = append(result, copy.VMA{
			Start: vma.Start,
			End:   vma.End,
			Size:  vma.MemSize,
			Perms: copy.Perm(vma.Perms),
		})
	}
	return result
}

// convertVMAs converts proc.VMA to elfcore.VMA
func convertVMAs(vmas []proc.VMA) []elfcore.VMA {
	var result []elfcore.VMA
	for _, vma := range vmas {
		result = append(result, elfcore.VMA{
			Start:      vma.Start,
			End:        vma.End,
			Perms:      elfcore.Perm(vma.Perms),
			Offset:     vma.Offset,
			Dev:        vma.Dev,
			Inode:      vma.Inode,
			Path:       vma.Path,
			Kind:       elfcore.VMAKind(vma.Kind),
			FileOffset: vma.FileOffset,
			MemSize:    vma.MemSize,
		})
	}
	return result
}
