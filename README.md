# livecore

A low-pause, capture-only Linux core dumper written in Go.

## Overview

livecore produces standards-compliant ELF ET_CORE core files from running Linux processes with minimal stop-the-world (STW) time by using iterative pre-copy passes and a short final delta copy.

## Features

- **Low pause time**: Uses soft-dirty tracking to minimize process interruption
- **Standards compliant**: Produces ELF core files readable by gdb, lldb, readelf
- **No restore functionality**: Capture-only tool
- **Cross-namespace support**: Works across PID namespaces with appropriate capabilities

## Requirements

- Linux x86-64 (aarch64 support planned)
- Go ≥ 1.25
- CAP_SYS_PTRACE capability
- Yama ptrace scope permitting

## Usage

```bash
livecore [flags] <pid> <output.core>
```

### Flags

- `-passes N`: Maximum pre-copy passes (default: 2)
- `-dirty-thresh PCT`: Stop when dirty < threshold (default: 5%)
- `-include-file-maps`: Dump private file-backed bytes (default: true)
- `-only-anon`: Dump only anonymous/private mappings
- `-respect-dontdump`: Honor MADV_DONTDUMP (default: true)
- `-notes all|minimal`: Which PT_NOTE sets to include (default: all)
- `-freeze method`: ptrace|cgroup (default: ptrace)
- `-concurrency N`: Concurrent read workers (default: runtime.GOMAXPROCS)
- `-iov-bytes SIZE`: Per-process_vm_readv batch size (default: 8MB)
- `-page-size SIZE`: Override page size (auto-detected by default)
- `-verbose`: Show progress and statistics

## Building

```bash
go build -o livecore ./cmd/livecore
```

## Testing

```bash
go test ./...
```

## License

MIT
