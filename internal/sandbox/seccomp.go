package sandbox

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func seccompAvailable() bool {
	// Check if seccomp is available via prctl.
	// PR_GET_SECCOMP should return 0 if available.
	ret, err := unix.PrctlRetInt(unix.PR_GET_SECCOMP, 0, 0, 0, 0)
	if err != nil {
		return false
	}
	return ret == 0
}

func applySeccomp() error {
	// Build a BPF program that blocks dangerous syscalls:
	// - SYS_PTRACE
	// - SYS_MEMFD_CREATE
	// - SYS_MOUNT
	// - SYS_UMOUNT2
	// - SYS_PROCESS_VM_READV
	// - SYS_PROCESS_VM_WRITEV
	// - SYS_KEXEC_LOAD
	//
	// Use SECCOMP_RET_ERRNO with EPERM rather than KILL for better error messages.
	//
	// Apply via prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog).
	// Requires PR_SET_NO_NEW_PRIVS to be set first (done by landlock).

	blocked := []uint32{
		unix.SYS_PTRACE,
		unix.SYS_MEMFD_CREATE,
		unix.SYS_MOUNT,
		unix.SYS_UMOUNT2,
		unix.SYS_PROCESS_VM_READV,
		unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_KEXEC_LOAD,
	}

	// Build BPF filter
	filter := buildSeccompFilter(blocked)

	prog := unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	}

	// PR_SET_NO_NEW_PRIVS should already be set by landlock
	// But set it again in case landlock was skipped
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl no_new_privs: %w", err)
	}

	_, _, errno := unix.Syscall(unix.SYS_SECCOMP,
		uintptr(unix.SECCOMP_MODE_FILTER),
		0,
		uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return fmt.Errorf("seccomp set filter: %w", errno)
	}
	return nil
}

// buildSeccompFilter constructs a BPF filter program that blocks the given
// syscall numbers with EPERM, allows everything else, and validates the
// architecture is x86_64.
//
// BPF program structure:
//  1. Load arch (seccomp_data.arch at offset 4)
//  2. Verify arch is AUDIT_ARCH_X86_64, kill if not
//  3. Load syscall number (seccomp_data.nr at offset 0)
//  4. For each blocked syscall: JEQ -> EPERM return
//  5. Default: ALLOW
func buildSeccompFilter(blocked []uint32) []unix.SockFilter {
	// BPF instruction constants
	const (
		bpfLdWAbs = 0x20 // BPF_LD | BPF_W | BPF_ABS
		bpfJmpJeq = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
		bpfRet    = 0x06 // BPF_RET | BPF_K

		seccompRetAllow = 0x7fff0000
		seccompRetErrno = 0x00050000
		eperm           = 0x1

		auditArchX86_64 = 0xC000003E

		// Offsets into seccomp_data struct
		offsetNr   = 0 // syscall number
		offsetArch = 4 // architecture
	)

	n := len(blocked)
	// Total instructions:
	//   1 (load arch) + 1 (check arch) + 1 (load nr) + n (comparisons) + 1 (allow) + 1 (errno)
	// = n + 5
	filter := make([]unix.SockFilter, 0, n+5)

	// Instruction 0: Load architecture from seccomp_data.arch (offset 4)
	filter = append(filter, unix.SockFilter{
		Code: bpfLdWAbs,
		K:    offsetArch,
	})

	// Instruction 1: If arch != AUDIT_ARCH_X86_64, jump to ERRNO return
	// jt=0 (fall through to next), jf=jump to errno return
	// The errno return is at index: n+4 (last instruction)
	// Current index is 1, so jf = (n+4) - (1+1) = n+2
	filter = append(filter, unix.SockFilter{
		Code: bpfJmpJeq,
		Jt:   0,
		Jf:   uint8(n + 2),
		K:    auditArchX86_64,
	})

	// Instruction 2: Load syscall number from seccomp_data.nr (offset 0)
	filter = append(filter, unix.SockFilter{
		Code: bpfLdWAbs,
		K:    offsetNr,
	})

	// Instructions 3..3+n-1: Compare against each blocked syscall
	for i, nr := range blocked {
		// If match, jump to ERRNO return (at index n+4)
		// Current index is 3+i, so jt = (n+4) - (3+i+1) = n - i
		// If no match, fall through (jf=0)
		filter = append(filter, unix.SockFilter{
			Code: bpfJmpJeq,
			Jt:   uint8(n - i),
			Jf:   0,
			K:    nr,
		})
	}

	// Instruction 3+n: Default ALLOW
	filter = append(filter, unix.SockFilter{
		Code: bpfRet,
		K:    seccompRetAllow,
	})

	// Instruction 4+n: ERRNO return (EPERM)
	filter = append(filter, unix.SockFilter{
		Code: bpfRet,
		K:    seccompRetErrno | eperm,
	})

	return filter
}
