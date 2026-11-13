package skillsafety

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Start suspended so the scanner cannot create a child before it belongs to our
// job. Closing the job also cleans up helpers after a normal scanner exit.
func runSpectorCommand(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("create scanner job: %w", err)
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fmt.Errorf("configure scanner job: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	cmd.Cancel = func() error {
		jobErr := windows.TerminateJobObject(job, 1)
		// Cancellation may race assignment to the job. The suspended process has
		// no descendants yet, and killing it directly covers that interval.
		processErr := cmd.Process.Kill()
		if jobErr != nil {
			return jobErr
		}
		if processErr != nil && !errors.Is(processErr, os.ErrProcessDone) {
			return processErr
		}
		return nil
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Every setup failure must reap the still-suspended process.
	abort := func(err error) error {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return abort(fmt.Errorf("open scanner process: %w", err))
	}
	err = windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		return abort(fmt.Errorf("assign scanner job: %w", err))
	}
	if err := resumeSpectorProcess(uint32(cmd.Process.Pid)); err != nil {
		return abort(fmt.Errorf("resume scanner process: %w", err))
	}
	return cmd.Wait()
}

func resumeSpectorProcess(processID uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != processID {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		_ = windows.CloseHandle(thread)
		return err
	}
	if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return errors.New("suspended scanner thread was not found")
	}
	return err
}
