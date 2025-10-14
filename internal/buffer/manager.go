package buffer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// offAndSize is a comparable map key to represent a memory region by
// its offset (in the target PID) and size.
type offAndSize struct {
	Offset uint64
	Size   uint64
}

// Manager manages a temporary file for buffering memory data.
type Manager struct {
	file *os.File

	mu          sync.Mutex            // Protects allocations and nextOffset
	allocations map[offAndSize]uint64 // VMA offset+size -> temp file offset
	nextOffset  uint64                // Next available offset in temp file
	fsBlockSize uint64                // Filesystem block size for alignment
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

	bm := &Manager{
		file:        tempFile,
		allocations: make(map[offAndSize]uint64),
		nextOffset:  0,
		fsBlockSize: fsBlockSize,
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

// GetOffsetForVMA returns the offset in the temp file for the given VMA
func (bm *Manager) GetOffsetForVMA(vmaStart, vmaSize uint64) uint64 {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	key := offAndSize{Offset: vmaStart, Size: vmaSize}

	if offset, ok := bm.allocations[key]; ok {
		return offset
	}

	// Allocate new space, aligned to filesystem block size
	alignedOffset := (bm.nextOffset + bm.fsBlockSize - 1) &^ (bm.fsBlockSize - 1)
	bm.allocations[key] = alignedOffset
	bm.nextOffset = alignedOffset + vmaSize

	return alignedOffset
}

// GetExistingOffsetForVMA returns the offset in the temp file for the given VMA if it exists
func (bm *Manager) GetExistingOffsetForVMA(vmaStart, vmaSize uint64) (tmpOffset uint64, ok bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	tmpOffset, ok = bm.allocations[offAndSize{Offset: vmaStart, Size: vmaSize}]
	return
}

// PunchHole punches a hole in the temp file to free disk space
func (bm *Manager) PunchHole(offset, length uint64) error {
	// Use fallocate with FALLOC_FL_PUNCH_HOLE | FALLOC_FL_KEEP_SIZE
	// This requires the file to be opened with O_RDWR
	err := unix.Fallocate(int(bm.file.Fd()), unix.FALLOC_FL_PUNCH_HOLE|unix.FALLOC_FL_KEEP_SIZE, int64(offset), int64(length))
	if err != nil {
		return fmt.Errorf("failed to punch hole at offset %d length %d: %w", offset, length, err)
	}
	return nil
}

// Close closes the BufferManager and cleans up the temp file
func (bm *Manager) Close() error {
	if bm.file != nil {
		bm.file.Close()
	}
	return nil
}

// ReadData reads data from the temp file at the given offset
func (bm *Manager) ReadData(offset, size uint64) ([]byte, error) {
	data := make([]byte, size)
	_, err := bm.file.ReadAt(data, int64(offset))
	if err != nil {
		return nil, fmt.Errorf("failed to read data at offset %d: %w", offset, err)
	}
	return data, nil
}

// WriteData writes data to the temp file at the given offset
func (bm *Manager) WriteData(offset uint64, data []byte) error {
	_, err := bm.file.WriteAt(data, int64(offset))
	if err != nil {
		return fmt.Errorf("failed to write data at offset %d: %w", offset, err)
	}
	return nil
}
