//go:build !unix

package builtin

import "os/exec"

// configureProcessGroup is a no-op on non-Unix platforms. V1 supports macOS and
// Linux only (PRD AS-015); the default exec cancellation (kill the direct
// process) plus cmd.WaitDelay still apply, but process-group teardown is a
// Unix-specific concern, so there is nothing portable to do here.
func configureProcessGroup(cmd *exec.Cmd) {}
