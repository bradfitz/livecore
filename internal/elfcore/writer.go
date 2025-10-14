package elfcore

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/bradfitz/livecore/internal/buffer"
)

// ELFWriter handles writing ELF core files
type ELFWriter struct {
	file          *os.File
	offset        uint64
	info          *CoreInfo
	outputFile    string // Base output file path for finding temp page files
	bufferManager *buffer.Manager
}

// NewELFWriter creates a new ELF core file writer
func NewELFWriter(filename string, info *CoreInfo, bufferManager *buffer.Manager) (*ELFWriter, error) {
	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create core file: %w", err)
	}

	return &ELFWriter{
		file:          file,
		offset:        0,
		info:          info,
		outputFile:    filename,
		bufferManager: bufferManager,
	}, nil
}

// Close closes the ELF writer
func (w *ELFWriter) Close() error {
	return w.file.Close()
}

// WriteCore writes the complete ELF core file
func (w *ELFWriter) WriteCore() error {
	// Calculate layout
	noteSize, noteOffset := w.calculateNoteLayout()
	loadSegments := w.calculateLoadSegments(noteOffset + noteSize)

	// Write ELF header
	if err := w.writeELFHeader(len(loadSegments) + 1); err != nil {
		return fmt.Errorf("failed to write ELF header: %w", err)
	}

	// Write program headers
	if err := w.writeProgramHeaders(noteOffset, noteSize, loadSegments); err != nil {
		return fmt.Errorf("failed to write program headers: %w", err)
	}

	// Write PT_NOTE segment
	if err := w.writeNoteSegment(); err != nil {
		return fmt.Errorf("failed to write note segment: %w", err)
	}

	// Write PT_LOAD segments
	if err := w.writeLoadSegments(loadSegments); err != nil {
		return fmt.Errorf("failed to write load segments: %w", err)
	}

	return nil
}

// calculateNoteLayout calculates the size and offset of the note segment.
func (w *ELFWriter) calculateNoteLayout() (noteSize, noteOffset uint64) {
	// Start after ELF header and program headers
	const phdrSize = 56                               // ELF64_Phdr
	phdrCount := uint64(len(w.getDumpableVMAs()) + 1) // +1 for PT_NOTE

	noteOffset = elfHeaderSize + phdrCount*phdrSize

	// Calculate note size
	noteSize = uint64(0)
	for _, note := range w.info.Notes {
		noteSize += w.calculateNoteSize(note)
	}

	return noteSize, noteOffset
}

// calculateLoadSegments calculates the layout of PT_LOAD segments
func (w *ELFWriter) calculateLoadSegments(noteEnd uint64) []LoadSegment {
	var segments []LoadSegment
	offset := noteEnd

	for _, vma := range w.getDumpableVMAs() {
		segment := LoadSegment{
			VMA:    vma,
			Offset: offset,
		}
		segments = append(segments, segment)
		offset += vma.Size()
	}

	return segments
}

const elfHeaderSize = 64

// writeELFHeader writes the ELF file header
func (w *ELFWriter) writeELFHeader(phnum int) error {
	header := make([]byte, elfHeaderSize)

	// ELF magic
	copy(header[0:4], []byte{0x7f, 'E', 'L', 'F'})

	// Class (64-bit)
	header[4] = ElfClass64

	// Data encoding (little-endian)
	header[5] = ElfData2LSB

	// Version
	header[6] = ElfVersion

	// OS/ABI (System V)
	header[7] = 0

	// ABI version
	header[8] = 0

	// Padding
	for i := 9; i < 16; i++ {
		header[i] = 0
	}

	// Type (ET_CORE)
	binary.LittleEndian.PutUint16(header[16:18], ET_CORE)

	// Machine (x86-64)
	binary.LittleEndian.PutUint16(header[18:20], GetELFMachine())

	// Version
	binary.LittleEndian.PutUint32(header[20:24], ElfVersion)

	// Entry point (0 for core files)
	binary.LittleEndian.PutUint64(header[24:32], 0)

	// Program header offset
	binary.LittleEndian.PutUint64(header[32:40], 64)

	// Section header offset (0 for core files)
	binary.LittleEndian.PutUint64(header[40:48], 0)

	// Flags
	binary.LittleEndian.PutUint32(header[48:52], 0)

	// ELF header size
	binary.LittleEndian.PutUint16(header[52:54], 64)

	// Program header entry size
	binary.LittleEndian.PutUint16(header[54:56], 56)

	// Number of program header entries
	binary.LittleEndian.PutUint16(header[56:58], uint16(phnum))

	// Section header entry size (0 for core files)
	binary.LittleEndian.PutUint16(header[58:60], 0)

	// Number of section header entries (0 for core files)
	binary.LittleEndian.PutUint16(header[60:62], 0)

	// Section header string table index (0 for core files)
	binary.LittleEndian.PutUint16(header[62:64], 0)

	_, err := w.file.WriteAt(header, 0)
	return err
}

// writeProgramHeaders writes the program header table
func (w *ELFWriter) writeProgramHeaders(noteOffset, noteSize uint64, loadSegments []LoadSegment) error {
	phdrOffset := int64(elfHeaderSize)

	// Write PT_NOTE header
	notePhdr := w.createNotePhdr(noteOffset, noteSize)
	if _, err := w.file.WriteAt(notePhdr, phdrOffset); err != nil {
		return err
	}
	phdrOffset += 56

	// Write PT_LOAD headers
	for _, segment := range loadSegments {
		loadPhdr := w.createLoadPhdr(segment)
		if _, err := w.file.WriteAt(loadPhdr, phdrOffset); err != nil {
			return err
		}
		phdrOffset += 56
	}

	return nil
}

// createNotePhdr creates a PT_NOTE program header
func (w *ELFWriter) createNotePhdr(offset, size uint64) []byte {
	phdr := make([]byte, 56)

	// Type (PT_NOTE)
	binary.LittleEndian.PutUint32(phdr[0:4], PT_NOTE)

	// Flags (readable)
	binary.LittleEndian.PutUint32(phdr[4:8], uint32(elf.PF_R))

	// Offset
	binary.LittleEndian.PutUint64(phdr[8:16], offset)

	// Virtual address (0 for notes)
	binary.LittleEndian.PutUint64(phdr[16:24], 0)

	// Physical address (0 for notes)
	binary.LittleEndian.PutUint64(phdr[24:32], 0)

	// File size
	binary.LittleEndian.PutUint64(phdr[32:40], size)

	// Memory size
	binary.LittleEndian.PutUint64(phdr[40:48], size)

	// Alignment
	binary.LittleEndian.PutUint64(phdr[48:56], 0)

	return phdr
}

// createLoadPhdr creates a PT_LOAD program header
func (w *ELFWriter) createLoadPhdr(segment LoadSegment) []byte {
	phdr := make([]byte, 56)

	// Type (PT_LOAD)
	binary.LittleEndian.PutUint32(phdr[0:4], PT_LOAD)

	// Flags
	flags := uint32(elf.PF_R)
	if segment.VMA.Perms&PermWrite != 0 {
		flags |= uint32(elf.PF_W)
	}
	if segment.VMA.Perms&PermExec != 0 {
		flags |= uint32(elf.PF_X)
	}
	binary.LittleEndian.PutUint32(phdr[4:8], flags)

	// Offset
	binary.LittleEndian.PutUint64(phdr[8:16], segment.Offset)

	// Virtual address
	binary.LittleEndian.PutUint64(phdr[16:24], uint64(segment.VMA.Start))

	// Physical address
	binary.LittleEndian.PutUint64(phdr[24:32], uint64(segment.VMA.Start))

	// File size
	binary.LittleEndian.PutUint64(phdr[32:40], segment.VMA.Size())

	// Memory size
	binary.LittleEndian.PutUint64(phdr[40:48], segment.VMA.Size())

	// Alignment
	binary.LittleEndian.PutUint64(phdr[48:56], 4096) // Page size

	return phdr
}

// writeNoteSegment writes the PT_NOTE segment
func (w *ELFWriter) writeNoteSegment() error {
	_, offset := w.calculateNoteLayout()

	for _, note := range w.info.Notes {
		if err := w.writeNote(note, &offset); err != nil {
			return fmt.Errorf("failed to write note %s: %w", note.Name, err)
		}
	}

	return nil
}

// writeLoadSegments writes the PT_LOAD segments
func (w *ELFWriter) writeLoadSegments(segments []LoadSegment) error {
	for _, segment := range segments {
		if err := w.writeLoadSegment(segment); err != nil {
			return fmt.Errorf("failed to write load segment for VMA %x-%x: %w",
				segment.VMA.Start, segment.VMA.End, err)
		}
	}
	return nil
}

// writeLoadSegment writes a single PT_LOAD segment
func (w *ELFWriter) writeLoadSegment(segment LoadSegment) error {
	// Read memory data from the BufferManager
	data, err := w.readMemoryData(segment.VMA)
	if err != nil {
		return fmt.Errorf("failed to read memory data for VMA %x-%x: %w",
			segment.VMA.Start, segment.VMA.End, err)
	}

	// Write the data to the ELF file
	_, err = w.file.WriteAt(data, int64(segment.Offset))
	if err != nil {
		return err
	}

	// Punch hole in the BufferManager to free disk space
	// Get the offset for this VMA in the BufferManager
	tmpOffset := w.bufferManager.GetOffsetForVMA(uint64(segment.VMA.Start), segment.VMA.Size())
	if err := w.bufferManager.PunchHole(tmpOffset, segment.VMA.Size()); err != nil {
		// Log but don't fail - hole punching is best effort
		fmt.Printf("Warning: failed to punch hole for VMA %x-%x: %v\n",
			segment.VMA.Start, segment.VMA.End, err)
	}

	return nil
}

// readMemoryData reads memory data for a VMA from the BufferManager
func (w *ELFWriter) readMemoryData(vma VMA) ([]byte, error) {
	// Get the offset for this VMA in the BufferManager (only if it exists)
	vmaOffset, ok := w.bufferManager.GetExistingOffsetForVMA(uint64(vma.Start), vma.Size())
	if !ok {
		return nil, fmt.Errorf("VMA %x-%x was not copied during pre-copy phase", vma.Start, vma.End)
	}

	// Read the data from the BufferManager
	data, err := w.bufferManager.ReadData(vmaOffset, vma.Size())
	if err != nil {
		// This is a real error - the VMA should have been copied during pre-copy
		return nil, fmt.Errorf("failed to read VMA data from buffer manager for %x-%x: %w", vma.Start, vma.End, err)
	}

	return data, nil
}

// getDumpableVMAs returns VMAs that should be included in the core dump
func (w *ELFWriter) getDumpableVMAs() []VMA {
	var dumpable []VMA
	for _, vma := range w.info.VMAs {
		if vma.IsDumpable() {
			dumpable = append(dumpable, vma)
		}
	}
	return dumpable
}

// LoadSegment represents a PT_LOAD segment
type LoadSegment struct {
	VMA    VMA
	Offset uint64
}
