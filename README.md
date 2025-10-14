# livecore

A low-pause, capture-only Linux core dumper written in Go.

## Overview

`livecore` produces core files from running Linux processes with minimal stop-the-world (STW) time by using iterative pre-copy passes and a short final delta copy.

The motivation is to run [`goref`](https://github.com/cloudwego/goref/) against processes
with hundreds of gigabytes of memory where `grf attach PID` is too slow and would result in visible downtime
for users. Instead, `goref` supports working from a core file, but tools like `gcore`
pause the process while they copy.

Instead `livecore` aims to generate subsecond pauses while the scanning phase before the pause and ELF core writing phase after the pause can take significantly longer.

## Requirements

- Linux x86-64 (aarch64 patches welcome)
- Go 1.25

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

## Apologies

This was my first (and so far only) vibe coding project, to see what all the
rage was about. It was ... interesting.

So be very suspicious of this tool.

The core dump it generates is incomplete and testing so far has only
involved `goref` consuming it.
