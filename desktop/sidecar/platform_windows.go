//go:build windows

package sidecar

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = modKernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = modKernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = modKernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = modKernel32.NewProc("TerminateJobObject")
	procCreateToolhelp32Snapshot = modKernel32.NewProc("CreateToolhelp32Snapshot")
	procThread32First            = modKernel32.NewProc("Thread32First")
	procThread32Next             = modKernel32.NewProc("Thread32Next")
	procOpenThread               = modKernel32.NewProc("OpenThread")
	procResumeThread             = modKernel32.NewProc("ResumeThread")
)

const (
	createNoWindow        = 0x08000000
	createNewProcessGroup = 0x00000200
	createSuspended       = 0x00000004

	th32csSnapThread    = 0x00000004
	threadSuspendResume = 0x0002

	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000

	processSetQuota  = 0x0100
	processTerminate = 0x0001
)

// jobObjectExtendedLimitInformationData mirrors
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION on 64-bit and 32-bit Windows.
type jobObjectExtendedLimitInformationData struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
	IoInfo                  [6]uint64
	ProcessMemoryLimit      uintptr
	JobMemoryLimit          uintptr
	PeakProcessMemoryUsed   uintptr
	PeakJobMemoryUsed       uintptr
}

// threadEntry32 mirrors THREADENTRY32.
type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
}

// configureCommand starts the child suspended, so attachProcess can put it
// in the Job Object before it runs any code or starts children of its own.
func configureCommand(cmd *exec.Cmd, options Options) {
	flags := uint32(createNewProcessGroup | createSuspended)
	if !options.ShowConsole {
		flags |= createNoWindow
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags, HideWindow: !options.ShowConsole}
}

type platformProcess struct {
	cmd      *exec.Cmd
	mu       sync.Mutex
	job      syscall.Handle
	released bool
}

// attachProcess puts the suspended child in a Job Object that kills it, and
// anything it starts, when the last job handle closes (which includes the app
// exiting or crashing), then resumes it. Because the child is still
// suspended at assignment, no descendant can start outside the job.
func attachProcess(cmd *exec.Cmd) (*platformProcess, error) {
	job, _, err := procCreateJobObjectW.Call(0, 0)
	if job == 0 {
		return nil, fmt.Errorf("CreateJobObjectW: %w", err)
	}
	var info jobObjectExtendedLimitInformationData
	info.LimitFlags = jobObjectLimitKillOnJobClose
	ok, _, err := procSetInformationJobObject.Call(job, jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	process, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return nil, fmt.Errorf("OpenProcess: %w", err)
	}
	defer syscall.CloseHandle(process)
	ok, _, err = procAssignProcessToJobObject.Call(job, uintptr(process))
	if ok == 0 {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return nil, fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	if err := resumeProcess(uint32(cmd.Process.Pid)); err != nil {
		_ = syscall.CloseHandle(syscall.Handle(job))
		return nil, err
	}
	return &platformProcess{cmd: cmd, job: syscall.Handle(job)}, nil
}

// resumeProcess resumes every thread of a process started suspended. A new
// process has exactly one thread until it runs.
func resumeProcess(pid uint32) error {
	snapshot, _, err := procCreateToolhelp32Snapshot.Call(th32csSnapThread, 0)
	if syscall.Handle(snapshot) == syscall.InvalidHandle {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry32{Size: uint32(unsafe.Sizeof(threadEntry32{}))}
	resumed := 0
	ok, _, err := procThread32First.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.OwnerProcessID == pid {
			thread, _, openErr := procOpenThread.Call(threadSuspendResume, 0, uintptr(entry.ThreadID))
			if thread == 0 {
				return fmt.Errorf("OpenThread: %w", openErr)
			}
			previous, _, resumeErr := procResumeThread.Call(thread)
			_ = syscall.CloseHandle(syscall.Handle(thread))
			if previous == ^uintptr(0) {
				return fmt.Errorf("ResumeThread: %w", resumeErr)
			}
			resumed++
		}
		entry.Size = uint32(unsafe.Sizeof(threadEntry32{}))
		ok, _, err = procThread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	if resumed == 0 {
		return fmt.Errorf("resume process %d: no threads found: %v", pid, err)
	}
	return nil
}

// interrupt is not offered on Windows: console control events need a shared
// console, and the sidecar has none. Stop falls through to kill.
func (p *platformProcess) interrupt() error {
	return errors.New("sidecar: graceful stop is not supported on Windows")
}

// kill terminates every process in the job. After release the job handle
// is closed (and its processes already ended), so kill does nothing.
func (p *platformProcess) kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.released {
		return
	}
	procTerminateJobObject.Call(uintptr(p.job), 1)
}

// release closes the job handle after the main process exits, which also
// ends any processes the sidecar left behind.
func (p *platformProcess) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.released {
		p.released = true
		_ = syscall.CloseHandle(p.job)
	}
}
