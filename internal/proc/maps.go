package proc

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// VMAKind represents the type of memory mapping.
type VMAKind int

const (
	VMAAnonymous VMAKind = 0
	VMAFile      VMAKind = 1
	VMAHeap      VMAKind = 2
	VMAStack     VMAKind = 3
	VMAShared    VMAKind = 4
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
	IsZero  bool     // True if this VMA should be zero-filled (no permissions)
	// Internal fields for tracking
	FileOffset uint64 // Offset in core file
	MemSize    uint64 // Size in core file
}

// ParseMaps parses /proc/<pid>/maps.
func ParseMaps(pid int) ([]VMA, error) {
	mapsPath := fmt.Sprintf("/proc/%d/maps", pid)
	file, err := os.Open(mapsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open maps: %w", err)
	}
	defer file.Close()

	var vmas []VMA
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		vma, err := parseMapsLine(line)
		if err != nil {
			return nil, fmt.Errorf("failed to parse maps line: %w", err)
		}
		vmas = append(vmas, vma)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read maps: %w", err)
	}

	// Parse smaps to get VmFlags for each VMA
	smapsInfo, err := ParseSMaps(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to parse smaps: %w", err)
	}

	// Merge VmFlags into VMAs
	for i := range vmas {
		if info, ok := smapsInfo[vmas[i].Start]; ok {
			vmas[i].VmFlags = info.VmFlags
		}
	}

	return vmas, nil
}

// parseMapsLine parses a single line from /proc/<pid>/maps.
func parseMapsLine(line string) (VMA, error) {
	parts := strings.Fields(line)
	if len(parts) < 5 {
		return VMA{}, fmt.Errorf("invalid maps line: %s", line)
	}

	// Parse address range
	addrRange := parts[0]
	addrParts := strings.Split(addrRange, "-")
	if len(addrParts) != 2 {
		return VMA{}, fmt.Errorf("invalid address range: %s", addrRange)
	}

	start, err := strconv.ParseUint(addrParts[0], 16, 64)
	if err != nil {
		return VMA{}, fmt.Errorf("invalid start address: %w", err)
	}

	end, err := strconv.ParseUint(addrParts[1], 16, 64)
	if err != nil {
		return VMA{}, fmt.Errorf("invalid end address: %w", err)
	}

	// Parse permissions
	perms := parts[1]
	var permFlags Perm
	if strings.Contains(perms, "r") {
		permFlags |= PermRead
	}
	if strings.Contains(perms, "w") {
		permFlags |= PermWrite
	}
	if strings.Contains(perms, "x") {
		permFlags |= PermExec
	}

	// Parse offset
	offset, err := strconv.ParseUint(parts[2], 16, 64)
	if err != nil {
		return VMA{}, fmt.Errorf("invalid offset: %w", err)
	}

	// Parse device
	dev := parts[3]
	devParts := strings.Split(dev, ":")
	if len(devParts) != 2 {
		return VMA{}, fmt.Errorf("invalid device: %s", dev)
	}

	major, err := strconv.ParseUint(devParts[0], 16, 64)
	if err != nil {
		return VMA{}, fmt.Errorf("invalid major device: %w", err)
	}

	minor, err := strconv.ParseUint(devParts[1], 16, 64)
	if err != nil {
		return VMA{}, fmt.Errorf("invalid minor device: %w", err)
	}

	devNum := (major << 8) | minor

	// Parse inode
	inode, err := strconv.ParseUint(parts[4], 10, 64)
	if err != nil {
		return VMA{}, fmt.Errorf("invalid inode: %w", err)
	}

	// Parse pathname (optional)
	var path string
	if len(parts) > 5 {
		path = strings.Join(parts[5:], " ")
	}

	// Determine VMA kind
	kind := determineVMAKind(path, uintptr(start), uintptr(end))

	// Check if this VMA should be zero-filled:
	// 1. No permissions (---p)
	// 2. Special kernel regions that can't be read via process_vm_readv
	isZero := perms == "---p" ||
		strings.Contains(path, "[vvar]") ||
		strings.Contains(path, "[vvar_vclock]") ||
		strings.Contains(path, "[vdso]") ||
		strings.Contains(path, "[vsyscall]")

	return VMA{
		Start:   uintptr(start),
		End:     uintptr(end),
		Perms:   permFlags,
		Offset:  offset,
		Dev:     devNum,
		Inode:   inode,
		Path:    path,
		Kind:    kind,
		IsZero:  isZero,
		MemSize: uint64(end - start),
	}, nil
}

// determineVMAKind determines the type of VMA based on its properties
func determineVMAKind(path string, start, end uintptr) VMAKind {
	if path == "" {
		return VMAAnonymous
	}

	// Check for special mappings
	if strings.Contains(path, "[heap]") {
		return VMAHeap
	}
	if strings.Contains(path, "[stack]") {
		return VMAStack
	}
	if strings.Contains(path, "[vdso]") || strings.Contains(path, "[vsyscall]") {
		return VMAAnonymous
	}

	// File-backed mapping
	return VMAFile
}

// ParseSMaps parses /proc/<pid>/smaps for additional VMA information.
func ParseSMaps(pid int) (map[uintptr]SMapsInfo, error) {
	smapsPath := fmt.Sprintf("/proc/%d/smaps", pid)
	file, err := os.Open(smapsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open smaps: %w", err)
	}
	defer file.Close()

	smapsInfo := make(map[uintptr]SMapsInfo)
	scanner := bufio.NewScanner(file)

	var currentVMA *SMapsInfo
	var currentStart uintptr

	for scanner.Scan() {
		line := scanner.Text()

		// Check if this is a new VMA header
		if strings.Contains(line, "-") && strings.Contains(line, " ") {
			// Save previous VMA if exists
			if currentVMA != nil {
				smapsInfo[currentStart] = *currentVMA
			}

			// Parse new VMA header
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				addrRange := parts[0]
				addrParts := strings.Split(addrRange, "-")
				if len(addrParts) == 2 {
					if start, err := strconv.ParseUint(addrParts[0], 16, 64); err == nil {
						currentStart = uintptr(start)
						currentVMA = &SMapsInfo{}
					}
				}
			}
		} else if currentVMA != nil {
			// Parse VMA properties
			parseSMapsProperty(line, currentVMA)
		}
	}

	// Save last VMA
	if currentVMA != nil {
		smapsInfo[currentStart] = *currentVMA
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read smaps: %w", err)
	}

	return smapsInfo, nil
}

// SMapsInfo contains additional information from smaps.
type SMapsInfo struct {
	Size       uint64
	RSS        uint64
	PSS        uint64
	Shared     uint64
	Private    uint64
	Referenced uint64
	Anonymous  uint64
	Swap       uint64
	VmFlags    []VMFlag
}

// parseSMapsProperty parses a single property line from smaps.
func parseSMapsProperty(line string, info *SMapsInfo) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return
	}

	key := parts[0]
	value := parts[1]

	switch key {
	case "Size:":
		if size, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.Size = size
		}
	case "Rss:":
		if rss, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.RSS = rss
		}
	case "Pss:":
		if pss, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.PSS = pss
		}
	case "Shared_Clean:", "Shared_Dirty:":
		if shared, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.Shared += shared
		}
	case "Private_Clean:", "Private_Dirty:":
		if private, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.Private += private
		}
	case "Referenced:":
		if ref, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.Referenced = ref
		}
	case "Anonymous:":
		if anon, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.Anonymous = anon
		}
	case "Swap:":
		if swap, err := strconv.ParseUint(value, 10, 64); err == nil {
			info.Swap = swap
		}
	case "VmFlags:":
		// Parse space-separated 2-character flags
		info.VmFlags = parseVmFlags(strings.Join(parts[1:], " "))
	}
}

// parseVmFlags parses space-separated 2-character flags into VMFlag slice.
func parseVmFlags(flagsStr string) []VMFlag {
	parts := strings.Fields(flagsStr)
	var flags []VMFlag
	for _, part := range parts {
		if len(part) == 2 {
			flags = append(flags, VMFlag{part[0], part[1]})
		}
		// Skip flags that aren't exactly 2 characters
	}
	return flags
}

// IsDumpable checks if a VMA should be included in the core dump.
func (vma *VMA) IsDumpable(includeFileMaps, onlyAnon, respectDontdump bool) bool {
	// Check if it's anonymous and we only want anonymous
	if onlyAnon && vma.Kind != VMAAnonymous {
		return false
	}

	// Check if it's file-backed and we don't want file maps
	if !includeFileMaps && vma.Kind == VMAFile {
		return false
	}

	// Check MADV_DONTDUMP if respectDontdump is true
	if respectDontdump {
		if slices.Contains(vma.VmFlags, vmFlagDD) {
			return false
		}
	}

	return true
}

// Size returns the size of the VMA.
func (vma *VMA) Size() uint64 {
	return vma.MemSize
}
