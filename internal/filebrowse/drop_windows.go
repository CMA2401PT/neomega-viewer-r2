package filebrowse

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
)

//go:embed drop_windows.ps1
var dropScript string

func NativeDropAvailable() bool {
	path := powerShellPath()
	if !filepath.IsAbs(path) {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}

func powerShellPath() string {
	return filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

// NativeDrop reads only the path returned by the shell's FileDrop payload.
func NativeDrop(ctx context.Context) (string, error) {
	const createNoWindow = 0x08000000
	var raw []byte
	for _, ch := range utf16.Encode([]rune(dropScript)) {
		raw = binary.LittleEndian.AppendUint16(raw, ch)
	}
	cmd := exec.CommandContext(ctx, powerShellPath(), "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	// Suppress the console without hiding the first WinForms window.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}
