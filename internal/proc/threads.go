package proc

import (
	"fmt"
	"os"
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
	// This is a simplified version - actual implementation would use
	// PTRACE_GETREGSET to collect all register sets

	// For now, return empty register data
	// In a real implementation, this would:
	// 1. Use PTRACE_GETREGSET with NT_PRSTATUS
	// 2. Use PTRACE_GETREGSET with NT_FPREGSET
	// 3. Use PTRACE_GETREGSET with NT_XSTATE
	// 4. Concatenate all the data

	return make([]byte, 1024), nil // Placeholder
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
	// PTRACE_CONT to resume the thread
	if err := unix.PtraceCont(tid, 0); err != nil {
		return fmt.Errorf("failed to continue thread %d: %w", tid, err)
	}

	// PTRACE_DETACH to detach from the thread
	if err := unix.PtraceDetach(tid); err != nil {
		return fmt.Errorf("failed to detach from thread %d: %w", tid, err)
	}

	return nil
}

// FreezeAllThreads freezes all threads in a process
func FreezeAllThreads(pid int) ([]Thread, error) {
	threads, err := ParseThreads(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to parse threads: %w", err)
	}

	var frozenThreads []Thread
	for _, thread := range threads {
		if err := FreezeThread(thread.Tid); err != nil {
			// If we can't freeze a thread, we should unfreeze the ones we did freeze
			for _, frozen := range frozenThreads {
				UnfreezeThread(frozen.Tid)
			}
			return nil, fmt.Errorf("failed to freeze thread %d: %w", thread.Tid, err)
		}
		frozenThreads = append(frozenThreads, thread)
	}

	return frozenThreads, nil
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
