package main

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func stateDirectory() (string, error) {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return "", errors.New("LOCALAPPDATA is not set")
	}
	return filepath.Join(dir, "fs-clip"), nil
}

func executableName() string { return "fs-clip.exe" }

func defaultBinaryDirectory() (string, error) {
	dir, err := stateDirectory()
	return filepath.Join(filepath.Dir(dir), "Programs", "fs-clip"), err
}

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

func acquireManagementLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func preparePlatformCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func prepareCommandLine() {
	if len(os.Args) < 2 {
		return
	}
	if _, err := windows.GetFileType(windows.Handle(os.Stdout.Fd())); err == nil {
		return
	}
	// Release builds have no console window; management commands can use the caller's console.
	attach := windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")
	if result, _, _ := attach.Call(uintptr(^uint32(0))); result == 0 {
		return
	}
	if handle, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE); err == nil {
		os.Stdout = os.NewFile(uintptr(handle), "stdout")
	}
	if handle, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil {
		os.Stderr = os.NewFile(uintptr(handle), "stderr")
	}
}

func taskIdentity() (string, string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", "", err
	}
	sid := user.User.Sid.String()
	return serviceLabel + "-" + sid, sid, nil
}

func xmlText(value string) string {
	var out strings.Builder
	xml.EscapeText(&out, []byte(value))
	return out.String()
}

func platformActivate(inst installation, p preferences) error {
	name, sid, err := taskIdentity()
	if err != nil {
		return err
	}
	arguments := "--watch-dir " + syscall.EscapeArg(p.WatchDir)
	task := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
<Triggers><LogonTrigger><Enabled>true</Enabled><UserId>%s</UserId></LogonTrigger></Triggers>
<Principals><Principal id="User"><UserId>%s</UserId><LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>
<Settings><MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy><DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries><StopIfGoingOnBatteries>false</StopIfGoingOnBatteries><AllowHardTerminate>true</AllowHardTerminate><StartWhenAvailable>true</StartWhenAvailable><Enabled>true</Enabled><Hidden>true</Hidden><ExecutionTimeLimit>PT0S</ExecutionTimeLimit><RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure></Settings>
<Actions Context="User"><Exec><Command>%s</Command><Arguments>%s</Arguments></Exec></Actions>
</Task>`, sid, sid, xmlText(inst.Binary), xmlText(arguments))
	file, err := os.CreateTemp("", "fs-clip-task-*.xml")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	words := utf16.Encode([]rune("\ufeff" + task))
	data := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(data[i*2:], word)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := platformCommand("schtasks.exe", "/Create", "/TN", name, "/XML", file.Name(), "/F"); err != nil {
		return err
	}
	_, err = platformCommand("schtasks.exe", "/Run", "/TN", name)
	return err
}

func platformStop(binaryPath string) error {
	name, _, err := taskIdentity()
	if err != nil {
		return err
	}
	// Ending an already stopped or absent task is harmless. Also stop legacy foreground instances.
	platformCommand("schtasks.exe", "/End", "/TN", name)
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	var currentSession uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &currentSession); err != nil {
		return err
	}
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == uint32(os.Getpid()) || !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), executableName()) {
			continue
		}
		var session uint32
		if windows.ProcessIdToSessionId(entry.ProcessID, &session) != nil || session != currentSession {
			continue
		}
		handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, entry.ProcessID)
		if err != nil {
			continue
		}
		buffer := make([]uint16, 32768)
		size := uint32(len(buffer))
		err = windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size)
		if err == nil && samePath(windows.UTF16ToString(buffer[:size]), binaryPath) {
			if err := windows.TerminateProcess(handle, 0); err != nil {
				// The scheduler may have completed termination after the process snapshot.
				if result, _ := windows.WaitForSingleObject(handle, 1000); result != windows.WAIT_OBJECT_0 {
					windows.CloseHandle(handle)
					return err
				}
			}
			result, err := windows.WaitForSingleObject(handle, 30000)
			if err != nil || result != windows.WAIT_OBJECT_0 {
				windows.CloseHandle(handle)
				return fmt.Errorf("waiting for FSClip to stop: result %d: %v", result, err)
			}
		}
		windows.CloseHandle(handle)
	}
	if err != nil && !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return err
	}
	return nil
}

func platformRemoveStartup(binary string) error {
	if err := platformStop(binary); err != nil {
		return err
	}
	name, _, err := taskIdentity()
	if err != nil {
		return err
	}
	if _, err := platformCommand("schtasks.exe", "/Query", "/TN", name, "/XML"); err != nil {
		return nil
	}
	_, err = platformCommand("schtasks.exe", "/Delete", "/TN", name, "/F")
	return err
}

func platformStatus() (string, error) {
	name, _, err := taskIdentity()
	if err != nil {
		return "", err
	}
	return platformCommand("schtasks.exe", "/Query", "/TN", name, "/FO", "LIST", "/V")
}

func platformEnsurePath(dir string) (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, err
	}
	defer key.Close()
	path, kind, err := key.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	for _, part := range strings.Split(path, ";") {
		if samePath(part, dir) {
			return false, nil
		}
	}
	if path != "" && !strings.HasSuffix(path, ";") {
		path += ";"
	}
	if kind == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", path+dir)
	} else {
		err = key.SetStringValue("Path", path+dir)
	}
	if err == nil {
		broadcastEnvironmentChange()
	}
	return err == nil, err
}

func platformRemovePath(dir string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	path, kind, err := key.GetStringValue("Path")
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	parts := strings.Split(path, ";")
	kept := parts[:0]
	for _, part := range parts {
		if !samePath(part, dir) {
			kept = append(kept, part)
		}
	}
	if kind == registry.EXPAND_SZ {
		err = key.SetExpandStringValue("Path", strings.Join(kept, ";"))
	} else {
		err = key.SetStringValue("Path", strings.Join(kept, ";"))
	}
	if err == nil {
		broadcastEnvironmentChange()
	}
	return err
}

func broadcastEnvironmentChange() {
	value, _ := windows.UTF16PtrFromString("Environment")
	var result uintptr
	windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW").Call(0xffff, 0x1a, 0, uintptr(unsafe.Pointer(value)), 2, 1000, uintptr(unsafe.Pointer(&result)))
}

func platformDeleteExecutable(path string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !samePath(self, path) {
		return removeIfExists(path)
	}
	file, err := os.CreateTemp("", "fs-clip-uninstall-*.cmd")
	if err != nil {
		return err
	}
	// Expansion is disabled; the target is passed in an environment variable so path characters stay literal.
	body := "@echo off\r\nsetlocal DisableDelayedExpansion\r\nset attempts=0\r\n:retry\r\ndel /f /q \"%FS_CLIP_REMOVE_TARGET%\" >nul 2>&1\r\nif not exist \"%FS_CLIP_REMOVE_TARGET%\" goto done\r\nset /a attempts+=1\r\nif %attempts% geq 30 goto done\r\nping.exe -n 2 127.0.0.1 >nul\r\ngoto retry\r\n:done\r\nendlocal\r\ndel \"%~f0\"\r\n"
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		os.Remove(file.Name())
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe", "/D", "/V:OFF", "/C", file.Name())
	cmd.Env = append(os.Environ(), "FS_CLIP_REMOVE_TARGET="+path)
	preparePlatformCommand(cmd)
	if err := cmd.Start(); err != nil {
		os.Remove(file.Name())
		return err
	}
	return cmd.Process.Release()
}
