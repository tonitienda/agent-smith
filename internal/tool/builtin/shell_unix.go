//go:build unix

package builtin

import (
	"errors"
	"os/exec"
	"syscall"
)

// configureProcessGroup puts the command in its own process group and replaces
// the context-cancel hook so cancellation kills the entire group, not just the
// direct shell process. This is what stops a shell's grandchildren (the real
// `sleep`, build, etc.) from surviving a timeout and holding the output pipe
// open. Unix-only by design: V1 targets macOS and Linux (PRD AS-015).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Guard the PID: syscall.Kill(-pid, ...) with pid <= 0 would signal our own
		// process group (kill(0, …) / kill(-1, …)) and take Agent Smith down with
		// the command. A successfully started child always has Pid > 0.
		if cmd.Process == nil || cmd.Process.Pid <= 0 {
			return nil
		}
		// Negative PID signals the whole process group led by the child.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == nil || errors.Is(err, syscall.ESRCH) {
			// Killed, or the group was already gone (the child is its own group
			// leader, so ESRCH means no descendant survives either).
			return nil
		}
		// A different failure (e.g. EPERM): the group may still be alive, so make a
		// best-effort attempt on the direct child and surface the original error so
		// the caller knows teardown might be incomplete.
		_ = cmd.Process.Kill()
		return err
	}
}
