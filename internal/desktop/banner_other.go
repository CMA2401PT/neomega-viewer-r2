//go:build !windows

package desktop

import "runtime"

func enableTerminalColors() bool { return true }

func osVersion() string { return runtime.GOOS }

func browserReport() []string { return nil }

// LaunchChoice 在其他平台上没有浏览器后端参数可传，直接用系统默认浏览器打开。
func LaunchChoice(address string, i int, diag *Diagnostics) (string, error) {
	diag.Printf("用系统默认浏览器打开 %s", address)
	OpenBrowser(address)
	return "系统默认浏览器", nil
}

// ListenChoices 只在 Windows 控制台里提供可点击的按钮。
func ListenChoices(pick func(int)) (restore func(), ok bool) { return func() {}, false }

// RestrictedLocation 只在 Windows 上检查目录权限。
func RestrictedLocation() (dir, sddl string) { return "", "" }
