package sandbox

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestSeccompAvailable(t *testing.T) {
	// seccompAvailable should return a bool without crashing.
	result := seccompAvailable()
	t.Logf("seccompAvailable() = %v", result)
}

func TestBuildSeccompFilter(t *testing.T) {
	blocked := []uint32{
		unix.SYS_PTRACE,
		unix.SYS_MEMFD_CREATE,
		unix.SYS_MOUNT,
		unix.SYS_UMOUNT2,
		unix.SYS_PROCESS_VM_READV,
		unix.SYS_PROCESS_VM_WRITEV,
		unix.SYS_KEXEC_LOAD,
	}

	filter := buildSeccompFilter(blocked)

	// Filter should not be empty
	if len(filter) == 0 {
		t.Fatal("buildSeccompFilter returned empty filter")
	}

	// Expected length: 1 (load arch) + 1 (check arch) + 1 (load nr) + N (comparisons) + 1 (allow) + 1 (errno)
	// = N + 5
	expectedLen := len(blocked) + 5
	if len(filter) != expectedLen {
		t.Errorf("filter length = %d, want %d", len(filter), expectedLen)
	}

	// First instruction should be BPF_LD|BPF_W|BPF_ABS loading arch (offset 4)
	if filter[0].Code != 0x20 {
		t.Errorf("filter[0].Code = 0x%x, want 0x20 (BPF_LD|BPF_W|BPF_ABS)", filter[0].Code)
	}
	if filter[0].K != 4 {
		t.Errorf("filter[0].K = %d, want 4 (offset of arch)", filter[0].K)
	}

	// Second instruction should check arch
	if filter[1].Code != 0x15 {
		t.Errorf("filter[1].Code = 0x%x, want 0x15 (BPF_JMP|BPF_JEQ|BPF_K)", filter[1].Code)
	}
	if filter[1].K != 0xC000003E {
		t.Errorf("filter[1].K = 0x%x, want 0xC000003E (AUDIT_ARCH_X86_64)", filter[1].K)
	}

	// Third instruction should load syscall number (offset 0)
	if filter[2].Code != 0x20 {
		t.Errorf("filter[2].Code = 0x%x, want 0x20 (BPF_LD|BPF_W|BPF_ABS)", filter[2].Code)
	}
	if filter[2].K != 0 {
		t.Errorf("filter[2].K = %d, want 0 (offset of nr)", filter[2].K)
	}

	// Last instruction should be BPF_RET with SECCOMP_RET_ERRNO | EPERM
	last := filter[len(filter)-1]
	if last.Code != 0x06 {
		t.Errorf("last.Code = 0x%x, want 0x06 (BPF_RET)", last.Code)
	}
	if last.K != 0x00050001 {
		t.Errorf("last.K = 0x%x, want 0x00050001 (SECCOMP_RET_ERRNO|EPERM)", last.K)
	}

	// Second to last instruction should be BPF_RET with SECCOMP_RET_ALLOW
	allow := filter[len(filter)-2]
	if allow.Code != 0x06 {
		t.Errorf("allow.Code = 0x%x, want 0x06 (BPF_RET)", allow.Code)
	}
	if allow.K != 0x7fff0000 {
		t.Errorf("allow.K = 0x%x, want 0x7fff0000 (SECCOMP_RET_ALLOW)", allow.K)
	}
}

func TestBuildSeccompFilterEmpty(t *testing.T) {
	filter := buildSeccompFilter(nil)

	// Even with no blocked syscalls, should have arch check + load nr + allow + errno
	// = 0 + 5 = 5
	if len(filter) != 5 {
		t.Errorf("filter length = %d, want 5", len(filter))
	}
}
