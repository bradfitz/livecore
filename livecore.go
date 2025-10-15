package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

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
	FixYama        bool
}

// parseFlags parses command line flags
func parseFlags() (*Config, error) {
	config := &Config{}

	flag.IntVar(&config.MaxPasses, "passes", 2, "maximum pre-copy passes")
	flag.Float64Var(&config.DirtyThreshold, "dirty-thresh", 5.0, "stop when dirty < threshold (percentage)")
	flag.IntVar(&config.Concurrency, "concurrency", runtime.GOMAXPROCS(0), "concurrent read workers")
	flag.BoolVar(&config.Verbose, "verbose", false, "show progress and statistics")
	flag.BoolVar(&config.FixYama, "fix-yama", false, "automatically fix yama.ptrace_scope sysctl and restore on exit")

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

// checkYamaSysctl returns the value of yama.ptrace_scope.
func checkYamaSysctl() (int, error) {
	data, err := os.ReadFile("/proc/sys/kernel/yama/ptrace_scope")
	if err != nil {
		return 0, fmt.Errorf("failed to read yama.ptrace_scope: %w", err)
	}

	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse yama.ptrace_scope value: %w", err)
	}

	return value, nil
}

// setYamaSysctl sets the yama.ptrace_scope sysctl value
func setYamaSysctl(value int) error {
	return os.WriteFile("/proc/sys/kernel/yama/ptrace_scope", []byte(fmt.Sprintf("%d\n", value)), 0644)
}

// fixYamaSysctl temporarily sets yama.ptrace_scope to 0 and returns a cleanup function
func fixYamaSysctl() (func(), error) {
	originalValue, err := checkYamaSysctl()
	if err != nil {
		return nil, err
	}

	if originalValue == 0 {
		// Already set to 0, no need to change
		return func() {}, nil
	}

	// Set to 0
	if err := setYamaSysctl(0); err != nil {
		return nil, fmt.Errorf("failed to set yama.ptrace_scope to 0: %w", err)
	}

	// Return cleanup function
	return func() {
		if err := setYamaSysctl(originalValue); err != nil {
			log.Printf("Warning: failed to restore yama.ptrace_scope to %d: %v", originalValue, err)
		}
	}, nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	config, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Check yama sysctl and handle it
	yamaValue, err := checkYamaSysctl()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var cleanupYama func()
	if yamaValue != 0 {
		if config.FixYama {
			// Automatically fix yama sysctl
			cleanupYama, err = fixYamaSysctl()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: failed to fix yama sysctl: %v\n", err)
				os.Exit(1)
			}
			log.Printf("Temporarily set yama.ptrace_scope to 0 (was %d)", yamaValue)
		} else {
			// Fail with instructions
			fmt.Fprintf(os.Stderr, "Error: yama.ptrace_scope is set to %d (non-zero), which prevents ptrace\n", yamaValue)
			fmt.Fprintf(os.Stderr, "To fix this, run: sudo sysctl kernel.yama.ptrace_scope=0\n")
			fmt.Fprintf(os.Stderr, "Or use the --fix-yama flag to automatically fix and restore it\n")
			os.Exit(1)
		}
	}

	// Set up signal handling to ensure cleanup on exit
	if cleanupYama != nil {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigChan
			log.Println("Received signal, cleaning up...")
			cleanupYama()
			os.Exit(1)
		}()
	}

	// Run livecore
	err = runLivecore(config)

	// Clean up yama sysctl if we modified it
	if cleanupYama != nil {
		cleanupYama()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runLivecore is the main function
func runLivecore(config *Config) error {
	if config.Verbose {
		log.Printf("livecore: dumping process %d to %s\n", config.Pid, config.OutputFile)
	}

	// Create BufferManager for efficient memory buffering
	bufferManager, err := buffer.NewBufferManager(config.OutputFile)
	if err != nil {
		return fmt.Errorf("failed to create buffer manager: %w", err)
	}
	defer bufferManager.Close()

	// Phase 1: Discovery
	if config.Verbose {
		log.Println("Phase 1: Discovery")
	}

	// Parse VMAs
	vmas, err := proc.ParseMaps(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to parse maps: %w", err)
	}

	if config.Verbose {
		log.Printf("Found %d VMAs", len(vmas))
	}

	// Parse threads
	threads, err := proc.ParseThreads(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to parse threads: %w", err)
	}

	if config.Verbose {
		log.Printf("Found %d threads", len(threads))
	}

	// Parse auxiliary vector
	_, err = proc.GetAuxv(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to get auxv: %w", err)
	}

	// Phase 2: Pre-copy (if enabled)
	if config.Verbose {
		log.Printf("MaxPasses: %d, DirtyThreshold: %.2f", config.MaxPasses, config.DirtyThreshold)
	}
	if config.MaxPasses > 0 {
		if config.Verbose {
			log.Println("Phase 2: Pre-copy")
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
			log.Printf("Pre-copy completed in %v", result.TotalTime)
		}
	}

	// Phase 3: Final stop and delta copy
	if config.Verbose {
		log.Println("Phase 3: Final stop and delta copy")
	}

	log.Printf("Starting freeze.")
	stopStart := time.Now()

	// Freeze all threads
	frozenThreads, err := proc.FreezeAllThreads(config.Pid)
	if err != nil {
		return fmt.Errorf("failed to freeze threads: %w", err)
	}

	log.Printf("[STW] Froze threads (took %v)", time.Since(stopStart))
	preThreads := time.Now()

	// Collect register state
	if err := proc.CollectThreadRegisters(frozenThreads); err != nil {
		proc.UnfreezeAllThreads(frozenThreads)
		return fmt.Errorf("failed to collect registers: %w", err)
	}

	if config.Verbose {
		log.Printf("[STW] Got thread registers (took %v)", time.Since(preThreads))
	}

	// Re-scan maps (authoritative at stop time)
	preMaps := time.Now()
	finalVMAs, err := proc.ParseMaps(config.Pid)
	if err != nil {
		proc.UnfreezeAllThreads(frozenThreads)
		return fmt.Errorf("failed to re-scan maps: %w", err)
	}

	if config.Verbose {
		log.Printf("[STW] Got final VMAs (took %v)", time.Since(preMaps))
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

	if config.Verbose {
		log.Printf("[STW] Unfrozen threads at STOP+%v", time.Since(stopStart))
	}

	stopTime := time.Since(stopStart)

	log.Printf("[STW] Done; total stop time was %v", stopTime)

	// Phase 4: Generate ELF core file
	if config.Verbose {
		log.Println("Phase 4: Generate ELF core file")
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
	preCore := time.Now()
	elfWriter, err := elfcore.NewELFWriter(config.OutputFile, coreInfo, bufferManager)
	if err != nil {
		return fmt.Errorf("failed to create ELF writer: %w", err)
	}
	defer elfWriter.Close()

	if err := elfWriter.WriteCore(); err != nil {
		return fmt.Errorf("failed to write core file: %w", err)
	}

	if config.Verbose {
		log.Printf("Core dump completed in %v", time.Since(preCore).Round(time.Millisecond))
	}

	return nil
}

// copyRemainingDirtyPages copies the remaining dirty pages after freeze
// This is the final delta copy - we only copy pages that are still dirty
// after the process has been frozen, ensuring we capture the final state
func copyRemainingDirtyPages(config *Config, vmas []proc.VMA, bufferManager *buffer.Manager) error {
	if config.Verbose {
		log.Println("Copying remaining dirty pages...")
	}

	// Create a new page map to scan for dirty pages after freeze
	pageMap := copy.NewPageMap(config.Pid)

	// Get current dirty pages (after freeze)
	preDisco := time.Now()
	currentDirtyPages, err := pageMap.GetDirtyPages(convertVMAsToCopy(vmas))
	if err != nil {
		return fmt.Errorf("failed to get current dirty pages: %w", err)
	}
	durDisco := time.Since(preDisco).Round(time.Millisecond)
	if config.Verbose {
		log.Printf("Found remaining dirty pages in %v", durDisco)
	}

	// Copy only the dirty pages using process_vm_readv
	// This is the minimal final copy to capture the exact state at freeze time
	if config.Verbose {
		log.Printf("Found %d dirty pages to copy", len(currentDirtyPages))
	}

	preCopy := time.Now()

	for pageAddr, vma := range currentDirtyPages {
		t0 := time.Now()
		if err := copyDirtyPage(config.Pid, pageAddr, *vma, bufferManager); err != nil {
			// Log but don't fail - some pages might not be readable
			if config.Verbose {
				log.Printf("Warning: failed to copy page at %x: %v", pageAddr, err)
			}
		}
		if config.Verbose {
			d := time.Since(t0)
			if d > 10*time.Millisecond {
				log.Printf("Copied final dirty page at %x in %v", pageAddr, d)
			}
		}
	}

	if config.Verbose {
		durCopy := time.Since(preCopy).Round(time.Millisecond)
		durTotal := time.Since(preDisco).Round(time.Millisecond)
		log.Printf("Copied final %d dirty pages in %v (discovery %v + copy %v)", len(currentDirtyPages), durTotal, durDisco, durCopy)
	}

	return nil
}

// copyDirtyPage copies a single dirty page to the BufferManager
func copyDirtyPage(pid int, pageAddr uintptr, vma copy.VMA, bufferManager *buffer.Manager) error {
	// Get page size
	pageSize := copy.GetPageSize()

	// Get the offset for this page in the temp file
	pageOffset := bufferManager.GetOffsetForVMA(uint64(vma.Start), vma.Size)

	// Get the mmap pointer for this page
	vmaBase, err := bufferManager.GetMmapPointer(pageOffset)
	if err != nil {
		return fmt.Errorf("failed to get mmap pointer: %w", err)
	}
	// Then adjust up to where in that VMA the page is
	mmapPtr := unsafe.Add(vmaBase, uintptr(pageAddr-vma.Start))

	// Copy the page directly to mmap
	err = copy.CopyMemoryToMmap(pid, pageAddr, uint64(pageSize), mmapPtr)
	if err != nil {
		// Skip pages that can't be read (like vsyscall, etc.)
		if err == unix.ENOENT || err == unix.EFAULT {
			return nil
		}
		return fmt.Errorf("failed to read page at %x: %w", pageAddr, err)
	}

	return nil
}

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
