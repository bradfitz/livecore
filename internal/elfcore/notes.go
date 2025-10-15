package elfcore

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	// prstatus_t structure for x86-64 (352 bytes total):
	// - pr_info (elf_siginfo_t): 16 bytes
	// - pr_cursig (short): 2 bytes
	// - pr_sigpend (unsigned long): 8 bytes
	// - pr_sighold (unsigned long): 8 bytes
	// - pr_pid (pid_t): 4 bytes
	// - pr_ppid (pid_t): 4 bytes
	// - pr_pgrp (pid_t): 4 bytes
	// - pr_sid (pid_t): 4 bytes
	// - pr_utime (timeval): 16 bytes
	// - pr_stime (timeval): 16 bytes
	// - pr_cutime (timeval): 16 bytes
	// - pr_cstime (timeval): 16 bytes
	// - pr_reg (pt_regs): 216 bytes (27 x86-64 general purpose registers)
	// - pr_fpvalid (unsigned long): 8 bytes

	prstatus := make([]byte, 352)

	// Fill signal info with zeros (we're not capturing signal state)
	// Offset 0-33: pr_info, pr_cursig, pr_sigpend, pr_sighold (34 bytes)

	// Set pr_pid (thread ID) at offset 34
	binary.LittleEndian.PutUint32(prstatus[34:38], uint32(thread.Tid))

	// Leave pr_ppid, pr_pgrp, pr_sid as zeros (offsets 38-50)

	// Leave timing info as zeros (offsets 50-114)

	// Copy register data starting at offset 114 (pr_reg field)
	// The registers from the thread should be in the correct format already
	if len(thread.Registers) > 0 {
		regOffset := 114
		regSize := 216 // Size of pt_regs on x86-64

		// Copy as much register data as we have, up to regSize
		copyLen := len(thread.Registers)
		if copyLen > regSize {
			copyLen = regSize
		}
		copy(prstatus[regOffset:regOffset+copyLen], thread.Registers)
	}

	// pr_fpvalid at offset 330 (8 bytes) - set to 0 (no FPU info in this note)
	// Already zero from make()

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

	// NOTE(bradfitz): don't really care for gorefs (grf) purposes, as these can't
	// contain pointers, IIUC.

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

	// NOTE(bradfitz): don't really care for gorefs (grf) purposes, as these can't
	// contain pointers, IIUC.

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
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return Note{}, fmt.Errorf("failed to read stat: %w", err)
	}

	// Read command line
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	cmdlineData, err := os.ReadFile(cmdlinePath)
	if err != nil {
		cmdlineData = []byte{} // Empty if can't read
	}

	// Parse stat data
	statStr := string(statData)
	fields := strings.Fields(statStr)
	if len(fields) < 4 {
		return Note{}, fmt.Errorf("invalid stat format")
	}

	// Create prpsinfo structure (136 bytes for x86-64)
	prpsinfo := make([]byte, 136)

	// pr_state (offset 0, 1 byte)
	if len(fields) > 2 {
		prpsinfo[0] = fields[2][0] // Process state character
	}

	// pr_sname (offset 1, 1 byte) - same as state
	prpsinfo[1] = prpsinfo[0]

	// pr_zomb (offset 2, 1 byte) - 1 if zombie
	if prpsinfo[0] == 'Z' {
		prpsinfo[2] = 1
	}

	// pr_nice (offset 3, 1 byte) - nice value
	if len(fields) > 18 {
		if nice, err := strconv.Atoi(fields[18]); err == nil {
			prpsinfo[3] = byte(nice)
		}
	}

	// pr_flag (offset 8, 8 bytes) - process flags
	if len(fields) > 8 {
		if flags, err := strconv.ParseUint(fields[8], 10, 64); err == nil {
			binary.LittleEndian.PutUint64(prpsinfo[8:16], flags)
		}
	}

	// pr_uid, pr_gid (offset 16, 4 bytes each) - set to 0 for now
	// These would need to be read from /proc/<pid>/status

	// pr_pid (offset 24, 4 bytes)
	binary.LittleEndian.PutUint32(prpsinfo[24:28], uint32(pid))

	// pr_ppid (offset 28, 4 bytes)
	if len(fields) > 3 {
		if ppid, err := strconv.Atoi(fields[3]); err == nil {
			binary.LittleEndian.PutUint32(prpsinfo[28:32], uint32(ppid))
		}
	}

	// pr_pgrp (offset 32, 4 bytes) - process group ID
	if len(fields) > 4 {
		if pgrp, err := strconv.Atoi(fields[4]); err == nil {
			binary.LittleEndian.PutUint32(prpsinfo[32:36], uint32(pgrp))
		}
	}

	// pr_sid (offset 36, 4 bytes) - session ID
	if len(fields) > 5 {
		if sid, err := strconv.Atoi(fields[5]); err == nil {
			binary.LittleEndian.PutUint32(prpsinfo[36:40], uint32(sid))
		}
	}

	// pr_fname (offset 40, 16 bytes) - executable name
	execName := "unknown"
	if len(fields) > 1 {
		// Remove parentheses from comm field
		comm := fields[1]
		if len(comm) > 2 && comm[0] == '(' && comm[len(comm)-1] == ')' {
			execName = comm[1 : len(comm)-1]
		}
	}
	if len(execName) > 15 {
		execName = execName[:15] // Truncate to fit
	}
	copy(prpsinfo[40:56], []byte(execName))

	// pr_psargs (offset 56, 80 bytes) - command line arguments
	if len(cmdlineData) > 0 {
		// Replace null bytes with spaces
		args := bytes.ReplaceAll(cmdlineData, []byte{0}, []byte{' '})
		// Trim trailing spaces
		args = bytes.TrimRight(args, " ")
		if len(args) > 79 {
			args = args[:79]
		}
		copy(prpsinfo[56:136], args)
	}

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

	// Validate that auxv data is properly formatted (should be pairs of 8-byte values)
	if len(auxvData)%16 != 0 {
		return Note{}, fmt.Errorf("invalid auxv data length: %d (should be multiple of 16)", len(auxvData))
	}

	// The auxv data should end with AT_NULL (type=0, value=0)
	// This is a 16-byte entry of all zeros at the end
	if len(auxvData) >= 16 {
		// Check if the last 16 bytes are all zeros (AT_NULL entry)
		lastEntry := auxvData[len(auxvData)-16:]
		allZeros := true
		for _, b := range lastEntry {
			if b != 0 {
				allZeros = false
				break
			}
		}
		if !allZeros {
			// Add AT_NULL terminator if missing
			auxvData = append(auxvData, make([]byte, 16)...)
		}
	} else if len(auxvData) == 0 {
		// If no auxv data, create minimal AT_NULL entry
		auxvData = make([]byte, 16)
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

	// Temporary buffer for binary encoding
	tmp := make([]byte, 8)

	// Write count (number of entries)
	binary.LittleEndian.PutUint64(tmp, uint64(len(fileTable)))
	buf.Write(tmp)

	// Write page size
	binary.LittleEndian.PutUint64(tmp, 4096)
	buf.Write(tmp)

	// Write file entries (start, end, file offset)
	for _, entry := range fileTable {
		binary.LittleEndian.PutUint64(tmp, uint64(entry.Start))
		buf.Write(tmp)
		binary.LittleEndian.PutUint64(tmp, uint64(entry.End))
		buf.Write(tmp)
		binary.LittleEndian.PutUint64(tmp, entry.FileOfs)
		buf.Write(tmp)
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
