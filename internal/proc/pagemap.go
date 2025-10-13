package proc

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// PageMap represents the soft-dirty view of pages
type PageMap struct {
	pid      int
	pageSize int
}

// NewPageMap creates a new PageMap for the given process
func NewPageMap(pid int) *PageMap {
	return &PageMap{
		pid:      pid,
		pageSize: syscall.Getpagesize(),
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

	for _, vma := range vmas {
		if err := pm.scanVMAForDirtyPages(vma, dirtyPages); err != nil {
			return nil, fmt.Errorf("failed to scan VMA %x-%x: %w", vma.Start, vma.End, err)
		}
	}

	return dirtyPages, nil
}

// scanVMAForDirtyPages scans a VMA for dirty pages
func (pm *PageMap) scanVMAForDirtyPages(vma VMA, dirtyPages map[uintptr]bool) error {
	pagemapPath := fmt.Sprintf("/proc/%d/pagemap", pm.pid)
	file, err := os.Open(pagemapPath)
	if err != nil {
		return fmt.Errorf("failed to open pagemap: %w", err)
	}
	defer file.Close()

	// Calculate page-aligned start and end
	start := vma.Start &^ uintptr(pm.pageSize-1)
	end := (vma.End + uintptr(pm.pageSize-1)) &^ uintptr(pm.pageSize-1)

	// Read pagemap entries for this VMA
	for addr := start; addr < end; addr += uintptr(pm.pageSize) {
		if err := pm.checkPageDirty(file, addr, dirtyPages); err != nil {
			return fmt.Errorf("failed to check page at %x: %w", addr, err)
		}
	}

	return nil
}

// checkPageDirty checks if a specific page is dirty
func (pm *PageMap) checkPageDirty(file *os.File, addr uintptr, dirtyPages map[uintptr]bool) error {
	// Calculate pagemap entry offset
	// Each pagemap entry is 8 bytes and represents one page
	entryOffset := int64(addr / uintptr(pm.pageSize) * 8)

	// Read the pagemap entry
	var entry [8]byte
	if _, err := file.ReadAt(entry[:], entryOffset); err != nil {
		return fmt.Errorf("failed to read pagemap entry: %w", err)
	}

	// Parse the entry
	// Bit 55 is the soft-dirty bit
	entryValue := *(*uint64)(unsafe.Pointer(&entry[0]))
	softDirty := (entryValue & (1 << 55)) != 0

	if softDirty {
		dirtyPages[addr] = true
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

// GetDirtyPagesForVMA returns dirty pages for a specific VMA
func (pm *PageMap) GetDirtyPagesForVMA(vma VMA) ([]uintptr, error) {
	var dirtyPages []uintptr

	pagemapPath := fmt.Sprintf("/proc/%d/pagemap", pm.pid)
	file, err := os.Open(pagemapPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open pagemap: %w", err)
	}
	defer file.Close()

	// Calculate page-aligned start and end
	start := vma.Start &^ uintptr(pm.pageSize-1)
	end := (vma.End + uintptr(pm.pageSize-1)) &^ uintptr(pm.pageSize-1)

	// Read pagemap entries for this VMA
	for addr := start; addr < end; addr += uintptr(pm.pageSize) {
		if err := pm.checkPageDirty(file, addr, map[uintptr]bool{}); err != nil {
			return nil, fmt.Errorf("failed to check page at %x: %w", addr, err)
		}

		// Check if this page is dirty
		entryOffset := int64(addr / uintptr(pm.pageSize) * 8)
		var entry [8]byte
		if _, err := file.ReadAt(entry[:], entryOffset); err != nil {
			continue // Skip if we can't read
		}

		entryValue := *(*uint64)(unsafe.Pointer(&entry[0]))
		softDirty := (entryValue & (1 << 55)) != 0

		if softDirty {
			dirtyPages = append(dirtyPages, addr)
		}
	}

	return dirtyPages, nil
}
