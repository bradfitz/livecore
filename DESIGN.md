# livecore Design Document

## Architecture Overview

livecore implements a low-pause core dumper using the following phases:

1. **Discovery**: Parse `/proc/<pid>/maps` to enumerate VMAs
2. **Pre-copy**: Iterative memory copying with soft-dirty tracking
3. **Final stop**: Brief pause to capture final delta
4. **ELF generation**: Write standards-compliant core file

## Core Components

### ELF Core Writer (`internal/elfcore/`)

- `writer.go`: Main ELF core file writer
- `notes.go`: PT_NOTE segment generation
- `segments.go`: PT_LOAD segment management

### Process Interface (`internal/proc/`)

- `maps.go`: Parse `/proc/<pid>/maps` and `/proc/<pid>/smaps`
- `pagemap.go`: Soft-dirty bit tracking via `/proc/<pid>/pagemap`
- `threads.go`: Thread enumeration and register collection
- `auxv.go`: Auxiliary vector parsing

### Memory Copying (`internal/copy/`)

- `precopy.go`: Iterative pre-copy with soft-dirty tracking
- `workers.go`: Concurrent memory reading workers
- `dirty.go`: Dirty page tracking and bitmap management

## Data Structures

```go
type VMA struct {
    Start, End uintptr
    Perms      Perm
    Offset     uint64
    Dev        uint64
    Inode      uint64
    Path       string
    Kind       VMAKind
}

type PageMap struct {
    // Soft-dirty view of pages
}

type Thread struct {
    Tid int
    // Register state
}
```

## Soft-Dirty Algorithm

1. Reset soft-dirty bits: `echo 4 > /proc/<pid>/clear_refs`
2. Copy pages using `process_vm_readv`
3. Read dirty bits from `/proc/<pid>/pagemap`
4. Repeat until dirty ratio < threshold or max passes reached

## Final Stop Process

1. Freeze all threads with `PTRACE_SEIZE` + `PTRACE_INTERRUPT`
2. Collect register state with `PTRACE_GETREGSET`
3. Copy remaining dirty pages
4. Unfreeze threads with `PTRACE_CONT`
5. Generate ELF core file

## ELF Core Format

- **PT_NOTE segment**: Contains all notes (registers, auxv, file table, etc.)
- **PT_LOAD segments**: One per VMA to be dumped
- **File layout**: Pre-allocated with accurate offsets

## Concurrency Model

- Worker pool for concurrent memory reading
- Batched `process_vm_readv` calls for efficiency
- Sparse bitmap per VMA for dirty page tracking

## Error Handling

- Graceful handling of disappearing VMAs
- Retry logic for failed memory reads
- Clear error messages for permission issues
