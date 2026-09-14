//go:build !unix

package tikz

import "os/exec"

// group is the process group treatment, which this platform does not have. The
// default is a kill of the one process, and WaitDelay is what stops a child of it
// holding the pipe open for longer than that.
func group(cmd *exec.Cmd) {}
