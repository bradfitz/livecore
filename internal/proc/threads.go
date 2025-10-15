package proc

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"

	"golang.org/x/sys/unix"
)

// Thread represents a thread in the target process
type Thread struct {
	Tid       int
	Registers []byte // Raw register data
}

// ParseThreads parses /proc/<pid>/task/* to enumerate threads
func ParseThreads(pid int) ([]Thread, error) {
	taskDir := fmt.Sprintf("/proc/%d/task", pid)
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read task directory: %w", err)
	}

	var threads []Thread
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		tid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // Skip non-numeric entries
		}

		thread := Thread{
			Tid: tid,
		}
		threads = append(threads, thread)
	}

	return threads, nil
}

// GetThreadRegisters collects register state for a thread using ptrace
func GetThreadRegisters(tid int) ([]byte, error) {
	var registers []byte

	// Collect general purpose registers using PTRACE_GETREGS
	regs, err := getGeneralRegisters(tid)
	if err != nil {
		return nil, fmt.Errorf("failed to get general registers: %w", err)
	}
	registers = append(registers, regs...)

	// Collect floating point registers using PTRACE_GETFPREGS
	fpregs, err := getFloatingPointRegisters(tid)
	if err != nil {
		// FP registers might not be available, continue without them
	} else {
		registers = append(registers, fpregs...)
	}

	return registers, nil
}

// getGeneralRegisters gets general purpose registers using PTRACE_GETREGS
func getGeneralRegisters(tid int) ([]byte, error) {
	// Get x86-64 registers using PtraceGetRegsAmd64
	var regs unix.PtraceRegsAmd64
	if err := unix.PtraceGetRegsAmd64(tid, &regs); err != nil {
		// Handle specific error cases
		if err == unix.ESRCH {
			// Thread no longer exists - this can happen if the thread exits
			// Return empty registers instead of failing
			return make([]byte, 216), nil
		}
		if err == unix.EPERM {
			return nil, fmt.Errorf("no permission to access thread %d", tid)
		}
		return nil, fmt.Errorf("failed to get registers for thread %d: %w", tid, err)
	}

	// Create register data in the exact format expected by ELF core files
	// This must match the user_regs_struct layout from the Linux kernel
	registers := make([]byte, 216) // Exact size for x86-64 elf_gregset_t

	// Use binary.Write for proper serialization
	buf := bytes.NewBuffer(registers[:0])

	// Write registers in the standard ELF core order (user_regs_struct)
	binary.Write(buf, binary.LittleEndian, regs.R15)
	binary.Write(buf, binary.LittleEndian, regs.R14)
	binary.Write(buf, binary.LittleEndian, regs.R13)
	binary.Write(buf, binary.LittleEndian, regs.R12)
	binary.Write(buf, binary.LittleEndian, regs.Rbp)
	binary.Write(buf, binary.LittleEndian, regs.Rbx)
	binary.Write(buf, binary.LittleEndian, regs.R11)
	binary.Write(buf, binary.LittleEndian, regs.R10)
	binary.Write(buf, binary.LittleEndian, regs.R9)
	binary.Write(buf, binary.LittleEndian, regs.R8)
	binary.Write(buf, binary.LittleEndian, regs.Rax)
	binary.Write(buf, binary.LittleEndian, regs.Rcx)
	binary.Write(buf, binary.LittleEndian, regs.Rdx)
	binary.Write(buf, binary.LittleEndian, regs.Rsi)
	binary.Write(buf, binary.LittleEndian, regs.Rdi)
	binary.Write(buf, binary.LittleEndian, regs.Orig_rax)
	binary.Write(buf, binary.LittleEndian, regs.Rip)
	binary.Write(buf, binary.LittleEndian, regs.Cs)
	binary.Write(buf, binary.LittleEndian, regs.Eflags)
	binary.Write(buf, binary.LittleEndian, regs.Rsp)
	binary.Write(buf, binary.LittleEndian, regs.Ss)

	// Add remaining fields to reach 216 bytes (27 * 8 bytes)
	// These are typically fs_base, gs_base, ds, es, fs, gs
	binary.Write(buf, binary.LittleEndian, uint64(0)) // fs_base
	binary.Write(buf, binary.LittleEndian, uint64(0)) // gs_base
	binary.Write(buf, binary.LittleEndian, uint64(0)) // ds
	binary.Write(buf, binary.LittleEndian, uint64(0)) // es
	binary.Write(buf, binary.LittleEndian, uint64(0)) // fs
	binary.Write(buf, binary.LittleEndian, uint64(0)) // gs

	return buf.Bytes(), nil
}

// getFloatingPointRegisters gets floating point registers using PTRACE_GETFPREGS
func getFloatingPointRegisters(tid int) ([]byte, error) {
	// For now, return empty FPU registers
	// TODO: Implement actual PTRACE_GETFPREGS call
	// The floating point registers are optional and can be empty

	// NOTE(bradfitz): don't really care for gorefs (grf) purposes, as these can't
	// contain pointers.

	fpregisters := make([]byte, 0)

	return fpregisters, nil
}

// FreezeThread freezes a thread using ptrace
func FreezeThread(tid int) error {
	// PTRACE_SEIZE to attach without stopping
	if err := unix.PtraceSeize(tid); err != nil {
		return fmt.Errorf("failed to seize thread %d: %w", tid, err)
	}

	// PTRACE_INTERRUPT to stop the thread
	if err := unix.PtraceInterrupt(tid); err != nil {
		return fmt.Errorf("failed to interrupt thread %d: %w", tid, err)
	}

	return nil
}

// UnfreezeThread unfreezes a thread using ptrace
func UnfreezeThread(tid int) error {
	// For seized threads, we can detach directly without resuming
	// PTRACE_DETACH will automatically resume the thread and detach
	if err := unix.PtraceDetach(tid); err != nil {
		// If thread no longer exists, that's okay - it already exited
		if err == unix.ESRCH {
			return nil
		}
		return fmt.Errorf("failed to detach from thread %d: %w", tid, err)
	}

	return nil
}

// FreezeAllThreads freezes all threads in a process and returns them sorted by tid.
func FreezeAllThreads(pid int) ([]Thread, error) {
	frozen := make(map[int]Thread) // by tid
	for {
		threads, err := ParseThreads(pid)
		if err != nil {
			return nil, fmt.Errorf("failed to parse threads: %w", err)
		}
		newCount := 0

		for _, thread := range threads {
			if _, ok := frozen[thread.Tid]; ok {
				// already frozen
				continue
			}
			if err := FreezeThread(thread.Tid); err != nil {
				// If we can't freeze a thread, we should unfreeze the ones we did freeze
				for tid := range frozen {
					UnfreezeThread(tid)
				}
				return nil, fmt.Errorf("failed to freeze thread %d: %w", thread.Tid, err)
			}

			frozen[thread.Tid] = thread
			newCount++
		}
		if newCount == 0 {
			ts := slices.Collect(maps.Values(frozen))
			slices.SortFunc(ts, func(a, b Thread) int {
				return cmp.Compare(a.Tid, b.Tid)
			})
			return ts, nil
		}
	}
}

// UnfreezeAllThreads unfreezes all threads in a process
func UnfreezeAllThreads(threads []Thread) error {
	var lastErr error
	for _, thread := range threads {
		if err := UnfreezeThread(thread.Tid); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// CollectThreadRegisters collects register state for all threads
func CollectThreadRegisters(threads []Thread) error {
	for i := range threads {
		registers, err := GetThreadRegisters(threads[i].Tid)
		if err != nil {
			// If thread no longer exists, skip it but continue with others
			if err == unix.ESRCH {
				// Thread exited, fill with empty registers
				threads[i].Registers = make([]byte, 0)
				continue
			}
			return fmt.Errorf("failed to get registers for thread %d: %w", threads[i].Tid, err)
		}
		threads[i].Registers = registers
	}
	return nil
}

// GetProcessInfo reads basic process information
func GetProcessInfo(pid int) (ProcessInfo, error) {
	var info ProcessInfo

	// Read comm from /proc/<pid>/comm
	commPath := fmt.Sprintf("/proc/%d/comm", pid)
	commData, err := os.ReadFile(commPath)
	if err != nil {
		return info, fmt.Errorf("failed to read comm: %w", err)
	}
	info.Comm = string(commData[:len(commData)-1]) // Remove newline

	// Read stat from /proc/<pid>/stat
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	statData, err := os.ReadFile(statPath)
	if err != nil {
		return info, fmt.Errorf("failed to read stat: %w", err)
	}

	// Parse stat data (simplified)
	info.Stat = string(statData)

	return info, nil
}

// ProcessInfo contains basic process information
type ProcessInfo struct {
	Comm string
	Stat string
}

// GetAuxv reads the auxiliary vector from /proc/<pid>/auxv
func GetAuxv(pid int) ([]byte, error) {
	auxvPath := fmt.Sprintf("/proc/%d/auxv", pid)
	auxvData, err := os.ReadFile(auxvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read auxv: %w", err)
	}
	return auxvData, nil
}
