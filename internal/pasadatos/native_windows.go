//go:build windows

package pasadatos

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

func runPowerShell(script string) ([]byte, error) {
	cmd := hiddenCommand("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-Command", script)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			message := strings.TrimSpace(string(exitErr.Stderr))
			if message != "" {
				return nil, errors.New(message)
			}
		}
		return nil, err
	}
	return output, nil
}

func nativePickFiles() ([]string, error) {
	script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.Encoding]::UTF8; Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.OpenFileDialog; $d.Multiselect=$true; $d.CheckFileExists=$true; $d.Filter='Todos los archivos (*.*)|*.*'; $d.Title='Seleccionar archivos para enviar con pasaDATOS'; if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){ @($d.FileNames) | ConvertTo-Json -Compress } else { '[]' }`
	out, err := runPowerShell(script)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el selector de archivos: %w", err)
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff"))
	if text == "" || text == "null" {
		return []string{}, nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(text), &paths); err == nil {
		return paths, nil
	}
	var one string
	if err := json.Unmarshal([]byte(text), &one); err == nil && one != "" {
		return []string{one}, nil
	}
	return nil, errors.New("el selector devolvió una respuesta inválida")
}

func nativePickFolder() (string, error) {
	script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[System.Text.Encoding]::UTF8; Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description='Elegí dónde guardar los archivos recibidos'; $d.ShowNewFolderButton=$true; if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){ $d.SelectedPath | ConvertTo-Json -Compress } else { '""' }`
	out, err := runPowerShell(script)
	if err != nil {
		return "", fmt.Errorf("no se pudo abrir el selector de carpeta: %w", err)
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(out), "\ufeff"))
	var folder string
	if err := json.Unmarshal([]byte(text), &folder); err != nil {
		return "", errors.New("el selector devolvió una respuesta inválida")
	}
	return folder, nil
}

func nativeOpenPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	var cmd *exec.Cmd
	if info.IsDir() {
		cmd = hiddenCommand("explorer.exe", path)
	} else {
		cmd = hiddenCommand("explorer.exe", "/select,"+path)
	}
	return cmd.Start()
}

func nativeOpenApp(rawURL string) error {
	candidates := []string{"msedge.exe"}
	for _, env := range []string{"PROGRAMFILES(X86)", "PROGRAMFILES", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base != "" {
			candidates = append(candidates, filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	for _, candidate := range candidates {
		path := candidate
		if !strings.Contains(candidate, string(filepath.Separator)) {
			if resolved, err := exec.LookPath(candidate); err == nil {
				path = resolved
			} else {
				continue
			}
		} else if _, err := os.Stat(candidate); err != nil {
			continue
		}
		cmd := hiddenCommand(path, "--app="+rawURL, "--start-maximized", "--disable-features=msEdgeSidebarV2")
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	return hiddenCommand("rundll32.exe", "url.dll,FileProtocolHandler", rawURL).Start()
}

func nativeSetAutoStart(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	if enabled {
		value := `"` + exe + `" --background`
		return hiddenCommand("reg.exe", "ADD", key, "/v", AppName, "/t", "REG_SZ", "/d", value, "/f").Run()
	}
	cmd := hiddenCommand("reg.exe", "DELETE", key, "/v", AppName, "/f")
	if err := cmd.Run(); err != nil {
		// Deleting an absent value is harmless.
		return nil
	}
	return nil
}
