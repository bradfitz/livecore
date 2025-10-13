package elfcore

import (
	"debug/elf"
	"slices"
)

// VMAKind represents the type of memory mapping.
type VMAKind int

const (
	VMAAnonymous VMAKind = iota
	VMAFile
	VMAHeap
	VMAStack
	VMAShared
)

// VMFlag constants
var vmFlagDD = VMFlag{'d', 'd'} // MADV_DONTDUMP flag

// Perm represents memory permissions.
type Perm uint8

const (
	PermRead  Perm = 1 << 0
	PermWrite Perm = 1 << 1
	PermExec  Perm = 1 << 2
)

// VMFlag represents a single memory advice flag (2 characters).
type VMFlag [2]byte

// VMA represents a virtual memory area.
type VMA struct {
	Start   uintptr
	End     uintptr
	Perms   Perm
	Offset  uint64
	Dev     uint64
	Inode   uint64
	Path    string
	Kind    VMAKind
	VmFlags []VMFlag // Memory advice flags from smaps
	// Internal fields for tracking
	FileOffset uint64 // Offset in core file
	MemSize    uint64 // Size in core file
}

// Thread represents a thread in the target process.
type Thread struct {
	Tid       int
	Registers []byte // Raw register data
}

// NoteType represents ELF note types.
type NoteType uint32

const (
	NT_PRSTATUS NoteType = 1
	NT_FPREGSET NoteType = 2
	NT_PRPSINFO NoteType = 3
	NT_AUXV     NoteType = 6
	NT_XSTATE   NoteType = 0x202
	NT_SIGINFO  NoteType = 0x53494749
	NT_FILE     NoteType = 0x46494c45
)

// Note represents an ELF note.
type Note struct {
	Name string
	Type NoteType
	Data []byte
}

// CoreInfo contains all information needed to generate a core file.
type CoreInfo struct {
	Pid     int
	Threads []Thread
	VMAs    []VMA
	Notes   []Note
	// File table for NT_FILE note
	FileTable []FileEntry
}

// FileEntry represents a file in the NT_FILE note.
type FileEntry struct {
	Start   uintptr
	End     uintptr
	FileOfs uint64
	Dev     uint64
	Inode   uint64
	Path    string
}

// ELF constants
const (
	ElfClass64  = 2
	ElfData2LSB = 1
	ElfVersion  = 1
	ET_CORE     = 4
	PT_NOTE     = 4
	PT_LOAD     = 1
)

// GetELFMachine returns the ELF machine type for the current architecture.
func GetELFMachine() uint16 {
	// x86-64
	return uint16(elf.EM_X86_64)
}

// IsDumpable returns true if the VMA should be included in the core dump.
func (vma *VMA) IsDumpable() bool {
	// Check for MADV_DONTDUMP flag
	if slices.Contains(vma.VmFlags, vmFlagDD) {
		return false
	}

	return true
}

// Size returns the size of the VMA.
func (vma *VMA) Size() uint64 {
	return vma.MemSize
}
