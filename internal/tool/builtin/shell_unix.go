//go:build unix

package builtin

import (
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
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill() // group gone already: fall back to the child.
		}
		return nil
	}
}
