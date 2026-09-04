//go:build windows

package proc

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var job windows.Handle

func init() {
	h, err := windows.CreateJobObject(nil, nil)
	if err == nil {
		job = h
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	}
}

// assignToJob places a child process in the panel's Job Object so it dies
// with the panel (KILL_ON_JOB_CLOSE). No-op if job creation failed.
func assignToJob(p *os.Process) {
	if job == 0 {
		return
	}
	_ = p.WithHandle(func(h uintptr) {
		_ = windows.AssignProcessToJobObject(job, windows.Handle(h))
	})
}
