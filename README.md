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
- `-concurrency N`: Concurrent read workers (default: runtime.GOMAXPROCS)
- `-verbose`: Show progress and statistics

## Installation

```bash
go install github.com/bradfitz/livecore@main
```

## Building from Source

```bash
git clone https://github.com/bradfitz/livecore.git
cd livecore
go build -o livecore .
```

## Testing

```bash
go test ./...
```

## License

MIT
