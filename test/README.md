# livecore Test Suite

Comprehensive validation testing for livecore core dump generation.

## Quick Start

### Prerequisites

- Go 1.25+ (livecore requirement)
- Standard Linux tools: `file`, `readelf`
- Optional: `gdb` (for GDB compatibility tests)
- Optional: `gcore` (for differential testing)
- Optional: `goref` (for goref compatibility tests)

### Running Tests

**Quick validation** (structural + GDB tests, ~30 seconds):
```bash
bash test/run_quick_tests.sh
```

**Full validation** (all test levels, ~5-10 minutes):
```bash
bash test/run_full_tests.sh
```

**Individual test levels**:
```bash
# Generate a core first
go run test/testprog.go &
TEST_PID=$!
./livecore -verbose $TEST_PID /tmp/test.core

# Then run specific validations
bash test/01_structural_validation.sh /tmp/test.core
bash test/02_gdb_validation.sh $TEST_PID /tmp/test.core
bash test/07_thread_validation.sh $TEST_PID /tmp/test.core

kill $TEST_PID
```

## Test Levels

### Level 1: Structural Validation
**Script**: `01_structural_validation.sh`
**Purpose**: Verify ELF format correctness
**Tests**:
- Valid ELF 64-bit file
- Core file identification
- readelf parsing
- PT_NOTE and PT_LOAD segments
- Required note types (NT_PRSTATUS, NT_AUXV)

**Example**:
```bash
bash test/01_structural_validation.sh /tmp/test.core
```

### Level 2: GDB Compatibility
**Script**: `02_gdb_validation.sh`
**Purpose**: Verify GDB can load and use the core
**Tests**:
- Core file loading
- Thread information access
- Register state access
- Basic navigation

**Example**:
```bash
bash test/02_gdb_validation.sh <pid> /tmp/test.core
```

### Level 3: Differential Validation
**Script**: `04_differential_validation.sh`
**Purpose**: Compare with gcore output
**Tests**:
- File size comparison
- PT_LOAD segment count
- Note type coverage

**Example**:
```bash
bash test/04_differential_validation.sh <pid>
```

### Level 4: Thread Validation
**Script**: `07_thread_validation.sh`
**Purpose**: Verify all threads captured
**Tests**:
- Thread count accuracy
- NT_PRSTATUS notes per thread

**Example**:
```bash
bash test/07_thread_validation.sh <pid> /tmp/test.core
```

## Test Programs

### testprog.go
Basic Go program for standard testing:
- 1MB memory allocation
- 4 goroutines
- Continuous activity
- Use for quick validation

```bash
go run test/testprog.go &
```

### large_process.go
Configurable large memory test:
- 1GB - 100GB+ allocation
- Continuous memory writes
- Use for scale testing

```bash
# 10GB test
go run test/large_process.go -size 10 &
```

### pause_detector.go
Performance monitoring:
- Detects process pauses
- Reports max pause time
- Use for STW time validation

```bash
go run test/pause_detector.go &
```

## Interpreting Results

### Success Indicators
✅ All structural tests pass
✅ GDB can load the core
✅ Thread count matches or exceeds live process
✅ STW pause < 1 second

### Expected Warnings
⚠️ GDB: "core file may not match specified executable" - Normal for go run
⚠️ "Skipping unreadable VMA" - Expected for kernel mappings
⚠️ More threads in core than /proc - Includes runtime threads

### Failure Indicators
❌ readelf cannot parse core
❌ Missing PT_NOTE or PT_LOAD segments
❌ No NT_PRSTATUS notes
❌ GDB completely fails to load

## Test Results

See `TEST_RESULTS.md` for comprehensive test findings on the current implementation.

## Troubleshooting

### "go: go.mod requires go >= 1.25"
Install Go 1.25 or use GOTOOLCHAIN:
```bash
export GOTOOLCHAIN=auto
```

### "permission denied" or "ptrace: Operation not permitted"
Set kernel.yama.ptrace_scope to 0:
```bash
sudo sysctl kernel.yama.ptrace_scope=0
# Or use livecore's --fix-yama flag
```

### Tests hang or fail
- Ensure test processes are running
- Check process hasn't exited
- Verify sufficient memory available
- Check ulimits for core size

### GDB warnings
"core file may not match specified executable" is normal for go run binaries. The core is still valid and usable.

## Adding New Tests

1. Create test script in `test/` directory
2. Follow naming convention: `NN_description.sh`
3. Return exit code 0 for pass, 1 for fail
4. Add to `run_full_tests.sh` if appropriate
5. Document in this README

## CI/CD Integration

For automated testing:

```bash
# In CI pipeline
bash test/run_quick_tests.sh

# For releases
bash test/run_full_tests.sh
```

Exit codes:
- 0: All tests passed
- 1: Some tests failed

## Known Limitations

- goref testing not yet implemented (requires goref installation)
- Differential testing requires manual setup
- Performance testing is basic
- No fuzzing or stress testing yet

See `TEST_PLAN.md` for full testing strategy and future improvements.
