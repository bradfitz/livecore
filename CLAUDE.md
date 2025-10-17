# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`livecore` is a low-pause Linux core dumper written in Go that generates core files from running processes with minimal stop-the-world time (subsecond pauses). It uses iterative pre-copy passes with soft-dirty tracking and a brief final delta copy. The primary use case is running `goref` against processes with hundreds of gigabytes of memory without visible downtime.

**Important caveat**: The author describes this as a "vibe coding project" and warns to "be very suspicious of this tool." Core dumps are incomplete and testing has only involved `goref` consuming them.

## Building and Testing

### Build Commands
```bash
# Build the binary (Linux x86-64 only)
go build -o livecore .

# Run with verbose output
./livecore -verbose -passes 2 -dirty-thresh 10 <pid> output.core
```

### Testing Commands
```bash
# Run all tests
go test -v ./...

# Run CI checks (includes linting, formatting, build, and integration tests)
./scripts/ci.sh

# Manual integration test
cd test
./validate.sh

# Test with HTTP server (requires grf tool)
cd test/httpserver
go build -o httpserver .
./httpserver 8080 &
../../livecore -verbose $! test.core
```

### CI Requirements
- The CI script (`scripts/ci.sh`) requires `grf` tool (`~/go/bin/grf`) to validate core dumps
- Tests validate that core dumps contain expected functions using `pprof`
- Server must remain responsive after core dump generation

## Architecture

The codebase follows a 4-phase architecture:

### Phase 1: Discovery (`internal/proc/`)
- **`maps.go`**: Parses `/proc/<pid>/maps` and `/proc/<pid>/smaps` to enumerate VMAs (Virtual Memory Areas)
  - Determines VMA kind (anonymous, file-backed, heap, stack)
  - Parses VmFlags including MADV_DONTDUMP
  - `IsDumpable()` method controls which VMAs are included in core dumps
- **`pagemap.go`**: Soft-dirty bit tracking via `/proc/<pid>/pagemap` (bit 55)
- **`threads.go`**: Thread enumeration and register collection using `PTRACE_GETREGSET`
- **`auxv.go`**: Auxiliary vector parsing from `/proc/<pid>/auxv`

### Phase 2: Pre-copy (`internal/copy/`)
- **`precopy.go`**: Iterative pre-copy engine with soft-dirty tracking
  - Clears soft-dirty bits via `echo 4 > /proc/<pid>/clear_refs`
  - Copies pages using `process_vm_readv` syscall
  - Repeats until dirty ratio < threshold or max passes reached
- **`workers.go`**: Concurrent memory reading workers (default: `GOMAXPROCS`)
- **`dirty.go`**: Dirty page tracking and bitmap management
- Uses `ProcessVMReadv` syscall for efficient bulk memory reading

### Phase 3: Final Stop (in `livecore.go`)
1. Freeze threads with `PTRACE_SEIZE` + `PTRACE_INTERRUPT` (see `proc.FreezeAllThreads`)
2. Collect register state with `PTRACE_GETREGSET`
3. Re-scan maps (authoritative at stop time)
4. Copy remaining dirty pages (final delta)
5. Unfreeze threads immediately with `PTRACE_CONT` (before ELF generation)

**Critical timing**: The process is unfrozen before Phase 4 to minimize pause time. The lengthy ELF file writing happens while the target process runs.

### Phase 4: ELF Generation (`internal/elfcore/`)
- **`writer.go`**: Main ELF core file writer
  - Pre-calculates offsets for ELF header, program headers, PT_NOTE, and PT_LOAD segments
  - Writes data from `BufferManager`'s mmap directly to avoid allocations
  - Punches holes in temp file to reclaim disk space after writing
- **`notes.go`**: PT_NOTE segment generation (registers, auxv, NT_FILE, etc.)
- **`segments.go`**: PT_LOAD segment management (one per dumpable VMA)

### Memory Buffering (`internal/buffer/`)
- **`manager.go`**: Efficient memory buffering using mmap
  - Creates temp file for staging copied memory
  - Maps memory regions with `mmap` for zero-copy operations
  - Tracks VMA offsets in temp file
  - Supports hole punching to reclaim disk space incrementally

## Key Data Flow

1. **Discovery**: `ParseMaps()` → VMAs → `ParseThreads()` → Threads
2. **Pre-copy**: VMAs → `PreCopyEngine.RunPreCopy()` → copies to `BufferManager` (mmap)
3. **Stop**: `FreezeAllThreads()` → collect registers → copy dirty pages → `UnfreezeAllThreads()`
4. **ELF write**: `BufferManager` (mmap) → `ELFWriter.WriteCore()` → output file

## Important Implementation Details

### Soft-Dirty Algorithm
- Reset: `echo 4 > /proc/<pid>/clear_refs`
- Copy all pages via `process_vm_readv`
- Read dirty bits from `/proc/<pid>/pagemap` (bit 55 = soft-dirty)
- Repeat until `dirty_ratio < threshold` or `max_passes` reached

### Yama Security
- Requires `kernel.yama.ptrace_scope=0` for ptrace operations
- Use `--fix-yama` flag to automatically set/restore this sysctl
- Without this, ptrace will fail with permission errors

### VMA Filtering
- `IsDumpable()` method in `internal/proc/maps.go` controls inclusion
- Respects MADV_DONTDUMP flag if configured
- Can filter file-backed vs anonymous mappings
- Some VMAs (vsyscall, etc.) are skipped if unreadable

### Error Handling
- Gracefully handles disappearing VMAs (process memory changes)
- Skips unreadable pages (returns `ENOENT`, `EFAULT`)
- Clear error messages for permission issues (yama ptrace_scope)

## Code Organization Patterns

- All `/proc` interactions are in `internal/proc/`
- All memory copying logic is in `internal/copy/`
- ELF-specific code is isolated in `internal/elfcore/`
- Main orchestration is in `livecore.go`
- Use `convertVMAs()`, `convertThreads()` functions to adapt between package types

## Development Notes

- **Platform**: Linux x86-64 only (aarch64 patches welcome)
- **Go version**: Requires Go 1.25
- **Dependencies**: Only `golang.org/x/sys/unix` for syscall wrappers
- **CGO**: Disabled (`CGO_ENABLED=0`)
- **Logging**: Uses `log.LstdFlags | log.Lmicroseconds` with `[STW]` markers for stop-the-world events
- **Performance**: Verbose mode logs operations > 10ms to identify slow paths
