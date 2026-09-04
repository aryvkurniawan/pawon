//go:build !windows

package proc

import "os"

func assignToJob(*os.Process) {}
