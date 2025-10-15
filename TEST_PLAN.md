# livecore Test Plan

## Overview

This test plan provides a comprehensive strategy for validating that livecore produces correct, complete, and usable core dumps with acceptable pause times. The verification is organized into six levels, progressing from basic structural validation to real-world scale testing.

## Testing Goals

1. **Correctness**: Verify the core dump is structurally valid and contains accurate data
2. **Completeness**: Ensure all required ELF segments and notes are present
3. **Compatibility**: Confirm standard tools (gdb, goref) can consume the output
4. **Performance**: Validate that stop-the-world time meets subsecond targets
5. **Scale**: Test with large memory processes (the primary use case)

## Test Levels

### Level 1: Structural Validation

**Objective**: Verify the core file is a structurally valid ELF core dump

**Tests**:
- Valid ELF 64-bit file format
- Identified as core file by `file` command
- Parseable by `readelf`
- Contains PT_NOTE segment
- Contains PT_LOAD segments
- Required note types present (NT_PRSTATUS, NT_AUXV, etc.)

**Tools**: `file`, `readelf`

**Script**: `test/01_structural_validation.sh`

**Success Criteria**: All structural checks pass

### Level 2: Tool Compatibility

**Objective**: Verify standard debugging tools can load and use the core dump

**Tests**:
- GDB can load the core file
- GDB can read thread information
- GDB can read register state
- GDB can read memory
- goref can analyze the core (primary use case)
- goref finds heap data
- goref finds goroutines

**Tools**: `gdb`, `goref`

**Scripts**:
- `test/02_gdb_validation.sh`
- `test/03_goref_validation.sh`

**Success Criteria**: All tools can successfully load and analyze the core

### Level 3: Correctness Validation

**Objective**: Verify captured data is accurate

**Tests**:
- Differential comparison with gcore output
- Memory region comparison
- Note section comparison
- Memory integrity spot checks

**Tools**: `gcore`, `readelf`, custom validators

**Scripts**:
- `test/04_differential_validation.sh`
- `test/05_memory_integrity_validation.go`

**Success Criteria**: Core dump matches gcore output within acceptable tolerances

### Level 4: Completeness Validation

**Objective**: Verify all necessary data is captured

**Tests**:
- All x86-64 registers present in notes
- Thread count matches live process
- All VMAs are captured
- Auxiliary vector present

**Scripts**:
- `test/06_register_validation.py`
- `test/07_thread_validation.sh`

**Success Criteria**: No missing critical data

### Level 5: Performance Validation

**Objective**: Verify stop-the-world time is acceptable

**Tests**:
- Measure actual STW pause during core dump
- Detect pauses > 100ms
- Verify pause < 1 second for typical workload
- Compare pause time across different memory sizes

**Programs**:
- `test/pause_detector.go`
- `test/08_pause_validation.sh`

**Success Criteria**: STW pause < 1 second for target workload size

### Level 6: Scale Testing

**Objective**: Verify functionality with large memory processes

**Tests**:
- Test with 1GB, 10GB, 50GB memory processes
- Verify pre-copy convergence
- Monitor dirty ratios across passes
- Ensure completion time is reasonable

**Programs**:
- `test/large_process.go`
- `test/09_scale_validation.sh`

**Success Criteria**: Successfully dumps large processes without errors

## Test Execution

### Quick Validation (Levels 1-2)

For rapid sanity checking:

```bash
./test/run_quick_tests.sh
```

Runs structural and tool compatibility tests (~30 seconds)

### Full Validation (All Levels)

For comprehensive verification:

```bash
./test/run_full_tests.sh
```

Runs all test levels (~5-10 minutes depending on scale tests)

### Individual Test Execution

Each test level can be run independently:

```bash
# Level 1: Structural
./test/01_structural_validation.sh <core_file>

# Level 2: GDB compatibility
./test/02_gdb_validation.sh <pid> <core_file>

# Level 3: Differential
./test/04_differential_validation.sh <pid>

# And so on...
```

## Test Programs

### testprog.go

Basic Go program for testing:
- Allocates 1MB memory
- Spawns 4 goroutines
- Continuous memory activity

### large_process.go

Large memory test program:
- Configurable memory size (1GB - 100GB+)
- Continuous memory writes to generate dirty pages
- Reports memory usage

### pause_detector.go

Performance monitoring program:
- Detects process pauses
- Reports maximum pause time
- Useful for STW time validation

## Expected Results

### Passing Criteria

A fully working implementation should achieve:

-  All Level 1 structural tests pass
-  GDB can load and navigate the core
-  goref can extract Go-specific data
-  <20% difference from gcore output size
-  All threads captured
-  All required registers present
-  STW pause < 1 second for 10GB process
-  Successfully handles 50GB+ processes

### Known Limitations

Based on README disclaimers:
- Core dumps may be "incomplete" (missing some note types)
- Testing so far only validates goref consumption
- x86-64 only (no ARM64 support)

## Test Infrastructure

### Directory Structure

```
test/
  01_structural_validation.sh
  02_gdb_validation.sh
  03_goref_validation.sh
  04_differential_validation.sh
  05_memory_integrity_validation.go
  06_register_validation.py
  07_thread_validation.sh
  08_pause_validation.sh
  09_scale_validation.sh
  run_quick_tests.sh
  run_full_tests.sh
  testprog.go (existing)
  large_process.go
  pause_detector.go
  validate.sh (existing - can be refactored)
```

## Continuous Testing

For ongoing validation:

1. Run quick tests on every commit
2. Run full tests before releases
3. Maintain corpus of test programs
4. Keep baseline cores for regression testing

## Troubleshooting Failed Tests

### Structural validation fails
- Check ELF header generation in `internal/elfcore/writer.go`
- Verify PT_NOTE and PT_LOAD segment creation

### GDB cannot load core
- Missing or incorrect note types
- Invalid register state in NT_PRSTATUS
- Check `internal/elfcore/notes.go`

### Differential tests show large differences
- VMAs not being captured correctly
- Memory copying issues in `internal/copy/precopy.go`
- Check soft-dirty bit tracking

### High STW times
- Pre-copy not converging (increase passes)
- Too high dirty threshold
- Check dirty ratio progression

### Scale tests fail
- Memory management issues
- Buffer manager problems in `internal/buffer/manager.go`
- Process VM limits

## Notes

- Tests require root/ptrace permissions (kernel.yama.ptrace_scope=0)
- Some tests may require goref to be installed
- Large memory tests may trigger OOM killer - adjust limits
- Performance tests should run on idle system for accurate results

## Future Improvements

- Add fuzzing for ELF writer
- Automated regression testing
- Cross-platform test suite (when ARM64 support added)
- Integration with CI/CD pipeline
