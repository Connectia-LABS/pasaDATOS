//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	user32            = syscall.NewLazyDLL("user32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
	procIsUserAnAdmin = shell32.NewProc("IsUserAnAdmin")
	procMessageBoxW   = user32.NewProc("MessageBoxW")
)

func platformIsElevated() bool {
	result, _, _ := procIsUserAnAdmin.Call()
	return result != 0
}

func platformElevate(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	operation, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	parameters, _ := syscall.UTF16PtrFromString(strings.Join(args, " "))
	result, _, callErr := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(parameters)), 0, 1)
	if result <= 32 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("Windows rechazó la elevación (código %d)", result)
	}
	return nil
}

func platformMessage(title, message string, isError bool) {
	caption, _ := syscall.UTF16PtrFromString(title)
	text, _ := syscall.UTF16PtrFromString(message)
	flags := uintptr(0x00000040) // MB_ICONINFORMATION
	if isError {
		flags = 0x00000010 // MB_ICONERROR
	}
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(caption)), flags)
}

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func psQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func platformCreateShortcuts(exe, icon, workingDir string) error {
	appData := os.Getenv("APPDATA")
	desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	startDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", appName)
	if err := os.MkdirAll(startDir, 0o755); err != nil {
		return err
	}
	shortcuts := []string{filepath.Join(startDir, appName+".lnk"), filepath.Join(desktop, appName+".lnk")}
	for _, shortcut := range shortcuts {
		script := `$ws=New-Object -ComObject WScript.Shell; $s=$ws.CreateShortcut(` + psQuote(shortcut) + `); $s.TargetPath=` + psQuote(exe) + `; $s.WorkingDirectory=` + psQuote(workingDir) + `; $s.IconLocation=` + psQuote(icon+",0") + `; $s.Description='Transferencia simple de archivos entre PC y celular'; $s.Save()`
		if output, err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

func platformRegisterInstall(exe, uninstaller, icon, installDir, version string) error {
	runKey := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	uninstallKey := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\pasaDATOS`
	commands := [][]string{
		{"ADD", runKey, "/v", appName, "/t", "REG_SZ", "/d", `"` + exe + `" --background`, "/f"},
		{"ADD", uninstallKey, "/v", "DisplayName", "/t", "REG_SZ", "/d", appName, "/f"},
		{"ADD", uninstallKey, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", version, "/f"},
		{"ADD", uninstallKey, "/v", "Publisher", "/t", "REG_SZ", "/d", "pasaDATOS", "/f"},
		{"ADD", uninstallKey, "/v", "DisplayIcon", "/t", "REG_SZ", "/d", icon, "/f"},
		{"ADD", uninstallKey, "/v", "InstallLocation", "/t", "REG_SZ", "/d", installDir, "/f"},
		{"ADD", uninstallKey, "/v", "UninstallString", "/t", "REG_SZ", "/d", `"` + uninstaller + `" --uninstall`, "/f"},
		{"ADD", uninstallKey, "/v", "NoModify", "/t", "REG_DWORD", "/d", "1", "/f"},
		{"ADD", uninstallKey, "/v", "NoRepair", "/t", "REG_DWORD", "/d", "1", "/f"},
	}
	for _, args := range commands {
		if output, err := hiddenCommand("reg.exe", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("registro: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}
	return nil
}

func platformAddFirewallRule(exe string) error {
	_ = platformRemoveFirewallRule()
	args := []string{"advfirewall", "firewall", "add", "rule", "name=pasaDATOS (red local)", "dir=in", "action=allow", "program=" + exe, "enable=yes", "profile=private", "protocol=TCP", "localport=8765"}
	output, err := hiddenCommand("netsh.exe", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func platformRemoveFirewallRule() error {
	_ = hiddenCommand("netsh.exe", "advfirewall", "firewall", "delete", "rule", "name=pasaDATOS (red local)").Run()
	return nil
}

func platformLaunch(exe string) error {
	cmd := hiddenCommand(exe)
	return cmd.Start()
}

func platformStopApp() error {
	_ = hiddenCommand("taskkill.exe", "/IM", appName+".exe", "/F", "/T").Run()
	return nil
}

func platformRemoveRegistration() error {
	_ = hiddenCommand("reg.exe", "DELETE", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", appName, "/f").Run()
	_ = hiddenCommand("reg.exe", "DELETE", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\pasaDATOS`, "/f").Run()
	return nil
}

func platformRemoveShortcuts() error {
	paths := []string{
		filepath.Join(os.Getenv("USERPROFILE"), "Desktop", appName+".lnk"),
		filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", appName),
	}
	for _, path := range paths {
		_ = os.RemoveAll(path)
	}
	return nil
}

func platformScheduleRemoval(paths []string) error {
	temp := os.TempDir()
	scriptPath := filepath.Join(temp, "pasadatos-remove-"+strconv.FormatInt(int64(os.Getpid()), 10)+".cmd")
	var lines []string
	lines = append(lines, "@echo off", "timeout /t 2 /nobreak >nul")
	for _, path := range paths {
		if strings.TrimSpace(path) != "" {
			lines = append(lines, `rmdir /s /q "`+strings.ReplaceAll(path, `"`, `""`)+`" 2>nul`)
		}
	}
	lines = append(lines, `del /f /q "%~f0"`)
	if err := os.WriteFile(scriptPath, []byte(strings.Join(lines, "\r\n")), 0o600); err != nil {
		return err
	}
	cmd := hiddenCommand("cmd.exe", "/C", scriptPath)
	if err := cmd.Start(); err != nil {
		return err
	}
	return nil
}

var _ = errors.Is
