//go:build unix

package tikz

import (
	"os/exec"
	"syscall"
)

// group puts a program in a process group of its own and kills the group rather
// than the program when the time runs out.
//
// TeX runs programs of its own, mktextfm and the like, and killing only the
// process this started leaves those holding the pipe it reads, so Compile waits
// for them anyway and a drawing that hangs takes as long as it was always going
// to. Killing the group is what makes the timeout a timeout.
func group(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
