package sandbox

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func landlockAvailable() bool {
	// Try LandlockCreateRuleset with empty attr to detect support.
	// Return false if ENOSYS or EOPNOTSUPP.
	attr := unix.LandlockRulesetAttr{
		Access_fs: unix.LANDLOCK_ACCESS_FS_READ_FILE |
			unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
			unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG |
			unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
			unix.LANDLOCK_ACCESS_FS_EXECUTE,
	}
	fd, err := landlockCreateRuleset(&attr)
	if err != nil {
		return false
	}
	unix.Close(fd)
	return true
}

func applyLandlock(cfg Config) error {
	// 1. Create ruleset FD with relevant access flags
	// 2. Add rule for WorkspaceDir: RW (read_file, write_file, read_dir, make_reg, make_dir, execute)
	// 3. Add rules for each ReadOnlyDir: RO (read_file, read_dir, execute)
	// 4. prctl(PR_SET_NO_NEW_PRIVS, 1)
	// 5. LandlockRestrictSelf(fd)
	// 6. Close fd
	// On error at any step: return descriptive error
	// If Landlock not available: log warning + return nil (best effort)

	accessFS := uint64(
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
			unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
			unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG |
			unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
			unix.LANDLOCK_ACCESS_FS_EXECUTE)

	attr := unix.LandlockRulesetAttr{Access_fs: accessFS}
	fd, err := landlockCreateRuleset(&attr)
	if err != nil {
		logWarning("landlock not available: %v", err)
		return nil
	}
	defer unix.Close(fd)

	// Workspace: full RW
	rwAccess := uint64(
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
			unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
			unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_MAKE_REG |
			unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
			unix.LANDLOCK_ACCESS_FS_EXECUTE)
	if err := landlockAddPathRule(fd, cfg.WorkspaceDir, rwAccess); err != nil {
		return fmt.Errorf("landlock workspace rule: %w", err)
	}

	// ReadOnly dirs
	roAccess := uint64(
		unix.LANDLOCK_ACCESS_FS_READ_FILE |
			unix.LANDLOCK_ACCESS_FS_READ_DIR |
			unix.LANDLOCK_ACCESS_FS_EXECUTE)
	for _, dir := range cfg.ReadOnlyDirs {
		if err := landlockAddPathRule(fd, dir, roAccess); err != nil {
			// Skip dirs that don't exist (e.g., /lib64 on some systems)
			logWarning("landlock rule for %s: %v (skipping)", dir, err)
			continue
		}
	}

	// No new privs + restrict self
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl no_new_privs: %w", err)
	}
	if err := landlockRestrictSelf(fd); err != nil {
		return fmt.Errorf("landlock restrict self: %w", err)
	}
	return nil
}

func landlockAddPathRule(rulesetFd int, path string, access uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer unix.Close(fd)

	pathBeneath := unix.LandlockPathBeneathAttr{
		Allowed_access: access,
		Parent_fd:      int32(fd),
	}
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_ADD_RULE,
		uintptr(rulesetFd),
		uintptr(unix.LANDLOCK_RULE_PATH_BENEATH),
		uintptr(unsafe.Pointer(&pathBeneath)))
	if errno != 0 {
		return fmt.Errorf("landlock add rule for %s: %w", path, errno)
	}
	return nil
}

func landlockCreateRuleset(attr *unix.LandlockRulesetAttr) (int, error) {
	fd, _, errno := unix.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(attr)),
		unsafe.Sizeof(*attr),
		0)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func landlockRestrictSelf(rulesetFd int) error {
	_, _, errno := unix.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF,
		uintptr(rulesetFd),
		0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
