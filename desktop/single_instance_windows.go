package main

import (
	"fmt"
	"golang.org/x/sys/windows"
)

// The Wails instance lock restores the existing window in the current session.
// This additional global mutex prevents running a second copy in another session.
func acquireMachineInstance() (func(), error) {
	name, _ := windows.UTF16PtrFromString(`Global\SSHGateway.Desktop.Instance`)
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return nil, fmt.Errorf("SSH Gateway is already running or its instance lock is unavailable")
	}
	return func() { windows.CloseHandle(handle) }, nil
}
