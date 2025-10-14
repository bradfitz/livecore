package buffer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// TmpOffset represents an offset in the temporary file.
type TmpOffset int64

// offAndSize is a comparable map key to represent a memory region by
// its offset (in the target PID) and size.
type offAndSize struct {
	Offset uint64
	Size   uint64
}

// Manager manages a temporary file for buffering memory data.
type Manager struct {
	file *os.File

	mu          sync.Mutex               // Protects allocations and nextOffset.
	allocations map[offAndSize]TmpOffset // VMA offset+size -> temp file offset.
	nextOffset  TmpOffset                // Next available offset in temp file.
	fsBlockSize uint64                   // Filesystem block size for alignment.

	// Mmap information for direct writes
	mmapData []byte // Mapped memory region.
	mmapSize int64  // Size of the mapped region.
}

// NewBufferManager creates a new BufferManager with a temporary file
func NewBufferManager(outputFile string) (*Manager, error) {
	// Create temp file in the same directory as output file
	outputDir := filepath.Dir(outputFile)
	tempFile, err := os.CreateTemp(outputDir, "livecore-buffer-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	os.Remove(tempPath) // so it doesn't persist after the program exits; we'll use the open fd only

	// Get filesystem block size for alignment
	fsBlockSize, err := getFilesystemBlockSize(tempFile)
	if err != nil {
		tempFile.Close()
		return nil, fmt.Errorf("failed to get filesystem block size: %w", err)
	}

	// Create a large initial file and mmap for direct writes
	mmapSize := int64(512 << 30) // 512GB
	if err := tempFile.Truncate(mmapSize); err != nil {
		tempFile.Close()
		return nil, fmt.Errorf("failed to create large temp file: %w", err)
	}
	mmapData, err := unix.Mmap(int(tempFile.Fd()), 0, int(mmapSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		tempFile.Close()
		return nil, fmt.Errorf("failed to mmap temp file: %w", err)
	}

	bm := &Manager{
		file:        tempFile,
		allocations: make(map[offAndSize]TmpOffset),
		nextOffset:  0,
		fsBlockSize: fsBlockSize,
		mmapData:    mmapData,
		mmapSize:    mmapSize,
	}

	return bm, nil
}

// getFilesystemBlockSize gets the filesystem block size for the given file
func getFilesystemBlockSize(file *os.File) (uint64, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Blksize), nil
}

// GetOffsetForVMA returns the offset in the temp file for the given VMA.
func (bm *Manager) GetOffsetForVMA(vmaStart, vmaSize uint64) TmpOffset {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	key := offAndSize{Offset: vmaStart, Size: vmaSize}

	if offset, ok := bm.allocations[key]; ok {
		return offset
	}

	// Allocate new space, aligned to filesystem block size
	alignedOffset := TmpOffset((bm.nextOffset + TmpOffset(bm.fsBlockSize) - 1) &^ (TmpOffset(bm.fsBlockSize) - 1))
	bm.allocations[key] = alignedOffset
	bm.nextOffset = alignedOffset + TmpOffset(vmaSize)

	// File is already large enough (512GB), no need to extend

	return alignedOffset
}

// GetMmapPointer returns a pointer to the mmap data at the given offset.
func (bm *Manager) GetMmapPointer(offset TmpOffset) (unsafe.Pointer, error) {
	if int64(offset) >= bm.mmapSize {
		return nil, fmt.Errorf("offset %d exceeds mmap size %d", offset, bm.mmapSize)
	}
	return unsafe.Pointer(&bm.mmapData[offset]), nil
}

// GetExistingOffsetForVMA returns the offset in the temp file for the given VMA if it exists.
func (bm *Manager) GetExistingOffsetForVMA(vmaStart, vmaSize uint64) (tmpOffset TmpOffset, ok bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	tmpOffset, ok = bm.allocations[offAndSize{Offset: vmaStart, Size: vmaSize}]
	return
}

// PunchHole punches a hole in the temp file to free disk space.
func (bm *Manager) PunchHole(offset TmpOffset, length uint64) error {
	// Use fallocate with FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE
	// This requires the file to be opened with O_RDWR
	err := unix.Fallocate(int(bm.file.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, int64(offset), int64(length))
	if err != nil {
		return fmt.Errorf("failed to punch hole at offset %d length %d: %w", offset, length, err)
	}
	return nil
}

// Close closes the BufferManager and cleans up the temp file.
func (bm *Manager) Close() error {
	if bm.mmapData != nil {
		unix.Munmap(bm.mmapData)
		bm.mmapData = nil
	}
	if bm.file != nil {
		bm.file.Close()
	}
	return nil
}

// ReadData reads data from the mmap buffer at the given offset.
func (bm *Manager) ReadData(offset TmpOffset, size uint64) ([]byte, error) {
	// Check bounds carefully to avoid SIGBUS
	if int64(offset) >= bm.mmapSize {
		return nil, fmt.Errorf("offset %d exceeds mmap size %d", offset, bm.mmapSize)
	}
	if int64(offset)+int64(size) > bm.mmapSize {
		return nil, fmt.Errorf("offset %d + size %d exceeds mmap size %d", offset, size, bm.mmapSize)
	}

	// Read directly from the mmap buffer
	data := make([]byte, size)
	copy(data, bm.mmapData[offset:offset+TmpOffset(size)])
	return data, nil
}

// WriteData writes data to the temp file at the given offset.
func (bm *Manager) WriteData(offset TmpOffset, data []byte) error {
	_, err := bm.file.WriteAt(data, int64(offset))
	if err != nil {
		return fmt.Errorf("failed to write data at offset %d: %w", offset, err)
	}
	return nil
}
