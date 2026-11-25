//go:build windows

package daemon

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// IsRocketLeagueRunning reports whether RocketLeague.exe is currently running on this host.
func IsRocketLeagueRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return false
	}
	for {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), "RocketLeague.exe") {
			return true
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return false
		}
	}
}
