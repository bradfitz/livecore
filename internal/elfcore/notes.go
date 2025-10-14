package elfcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// NoteWriter handles writing ELF notes
type NoteWriter struct {
	buf bytes.Buffer
}

// NewNoteWriter creates a new note writer
func NewNoteWriter() *NoteWriter {
	return &NoteWriter{}
}

func padUpTo4Bytes(n int) int {
	return (n + 3) &^ 3
}

// WriteNote writes a note to the buffer
func (nw *NoteWriter) WriteNote(name string, noteType NoteType, data []byte) error {
	// Calculate sizes
	nameSize := padUpTo4Bytes(len(name) + 1) // +1 for null terminator
	dataSize := padUpTo4Bytes(len(data))

	// Write note header
	header := make([]byte, 12)
	binary.LittleEndian.PutUint32(header[0:4], uint32(nameSize))
	binary.LittleEndian.PutUint32(header[4:8], uint32(dataSize))
	binary.LittleEndian.PutUint32(header[8:12], uint32(noteType))

	if _, err := nw.buf.Write(header); err != nil {
		return err
	}

	// Write name (null-terminated and padded)
	nw.buf.WriteString(name)
	for range nameSize - len(name) {
		nw.buf.WriteByte(0)
	}

	if len(data) > 0 {
		nw.buf.Write(data)
		for range dataSize - len(data) {
			nw.buf.WriteByte(0)
		}
	}

	return nil
}

// Bytes returns the written notes as bytes
func (nw *NoteWriter) Bytes() []byte {
	return nw.buf.Bytes()
}

// Size returns the total size of written notes
func (nw *NoteWriter) Size() int {
	return nw.buf.Len()
}

// calculateNoteSize calculates the size of a note
func (w *ELFWriter) calculateNoteSize(note Note) uint64 {
	nameSize := len(note.Name) + 1
	if nameSize%4 != 0 {
		nameSize = ((nameSize + 3) / 4) * 4
	}

	dataSize := len(note.Data)
	if dataSize%4 != 0 {
		dataSize = ((dataSize + 3) / 4) * 4
	}

	return uint64(12 + nameSize + dataSize) // 12 bytes for header
}

// writeNote writes a single note to the file
func (w *ELFWriter) writeNote(note Note, offset *uint64) error {
	nw := NewNoteWriter()
	if err := nw.WriteNote(note.Name, note.Type, note.Data); err != nil {
		return err
	}

	_, err := w.file.WriteAt(nw.Bytes(), int64(*offset))
	*offset += uint64(nw.Size())
	return err
}

// CreateCoreNotes creates all the notes for a core file
func CreateCoreNotes(pid int, threads []Thread, fileTable []FileEntry) ([]Note, error) {
	var notes []Note

	// NT_PRSTATUS for each thread
	for _, thread := range threads {
		prstatus := createPRStatusNote(thread)
		notes = append(notes, prstatus)
	}

	// NT_FPREGSET for each thread
	for _, thread := range threads {
		fpregset := createFPRegsetNote(thread)
		notes = append(notes, fpregset)
	}

	// NT_XSTATE for each thread
	for _, thread := range threads {
		xstate := createXStateNote(thread)
		notes = append(notes, xstate)
	}

	// NT_PRPSINFO
	prpsinfo, err := createPRPSInfoNote(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to create PRPSINFO note: %w", err)
	}
	notes = append(notes, prpsinfo)

	// NT_AUXV
	auxv, err := createAuxvNote(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to create AUXV note: %w", err)
	}
	notes = append(notes, auxv)

	// NT_FILE
	if len(fileTable) > 0 {
		file := createFileNote(fileTable)
		notes = append(notes, file)
	}

	return notes, nil
}

// createPRStatusNote creates a NT_PRSTATUS note
func createPRStatusNote(thread Thread) Note {
	// This is a simplified version - actual implementation would parse
	// the register data from the thread
	prstatus := make([]byte, 336) // Size of prstatus_t on x86-64

	// Fill with zeros for now - actual register data would be populated
	// from the thread's register state

	return Note{
		Name: "CORE",
		Type: NT_PRSTATUS,
		Data: prstatus,
	}
}

// createFPRegsetNote creates a NT_FPREGSET note
func createFPRegsetNote(thread Thread) Note {
	// FPU register set - 512 bytes for x87 + SSE
	fpregset := make([]byte, 512)

	return Note{
		Name: "CORE",
		Type: NT_FPREGSET,
		Data: fpregset,
	}
}

// createXStateNote creates a NT_XSTATE note
func createXStateNote(thread Thread) Note {
	// XSAVE state - variable size
	xstate := make([]byte, 1024) // Simplified size

	return Note{
		Name: "CORE",
		Type: NT_XSTATE,
		Data: xstate,
	}
}

// createPRPSInfoNote creates a NT_PRPSINFO note
func createPRPSInfoNote(pid int) (Note, error) {
	// Read process info from /proc/<pid>/stat
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	_, err := os.ReadFile(statPath)
	if err != nil {
		return Note{}, fmt.Errorf("failed to read stat: %w", err)
	}

	// Parse stat data and create prpsinfo structure
	// This is simplified - actual implementation would parse the stat file
	prpsinfo := make([]byte, 136) // Size of prpsinfo_t

	// Fill with basic info
	copy(prpsinfo[0:16], []byte("test_program")) // pr_fname

	return Note{
		Name: "CORE",
		Type: NT_PRPSINFO,
		Data: prpsinfo,
	}, nil
}

// createAuxvNote creates a NT_AUXV note
func createAuxvNote(pid int) (Note, error) {
	// Read auxiliary vector from /proc/<pid>/auxv
	auxvPath := fmt.Sprintf("/proc/%d/auxv", pid)
	auxvData, err := os.ReadFile(auxvPath)
	if err != nil {
		return Note{}, fmt.Errorf("failed to read auxv: %w", err)
	}

	return Note{
		Name: "CORE",
		Type: NT_AUXV,
		Data: auxvData,
	}, nil
}

// createFileNote creates a NT_FILE note
func createFileNote(fileTable []FileEntry) Note {
	var buf bytes.Buffer

	// Write count
	binary.LittleEndian.PutUint64(buf.Bytes(), uint64(len(fileTable)))

	// Write file entries
	for _, entry := range fileTable {
		binary.LittleEndian.PutUint64(buf.Bytes(), uint64(entry.Start))
		binary.LittleEndian.PutUint64(buf.Bytes(), uint64(entry.End))
		binary.LittleEndian.PutUint64(buf.Bytes(), entry.FileOfs)
		binary.LittleEndian.PutUint64(buf.Bytes(), entry.Dev)
		binary.LittleEndian.PutUint64(buf.Bytes(), entry.Inode)
	}

	// Write path strings
	for _, entry := range fileTable {
		buf.WriteString(entry.Path)
		buf.WriteByte(0) // null terminator
	}

	return Note{
		Name: "CORE",
		Type: NT_FILE,
		Data: buf.Bytes(),
	}
}
