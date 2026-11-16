//go:build !windows

package desktop

func enableTerminalColors() bool { return true }

// LaunchApp 在其他平台上没有浏览器后端参数可传，直接用系统默认浏览器打开。
func LaunchApp(address, angle string) string {
	OpenBrowser(address)
	return "系统默认浏览器"
}
