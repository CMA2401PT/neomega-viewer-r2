//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

// 旧版 Windows 控制台默认不解释 ANSI 转义，需要先打开虚拟终端处理。
func enableTerminalColors() bool {
	const virtualTerminalProcessing = 0x0004
	kernel := syscall.NewLazyDLL("kernel32.dll")
	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if ok, _, _ := kernel.NewProc("GetConsoleMode").Call(uintptr(handle), uintptr(unsafe.Pointer(&mode))); ok == 0 {
		return false
	}
	ok, _, _ := kernel.NewProc("SetConsoleMode").Call(uintptr(handle), uintptr(mode|virtualTerminalProcessing))
	return ok != 0
}

// browserCandidates 列出支持 --app 与 --use-angle 的浏览器。
func browserCandidates() []string {
	var list []string
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
		if base == "" {
			continue
		}
		list = append(list,
			filepath.Join(base, "Google/Chrome/Application/chrome.exe"),
			filepath.Join(base, "Microsoft/Edge/Application/msedge.exe"),
		)
	}
	return list
}

// ensureProfile 找一个当前进程真的能写的浏览器配置目录。
// 顺序：exe 旁的 runtime（便携安装或在受限目录里运行时也能写）、%LOCALAPPDATA%、临时目录。
// 配置目录不可写时浏览器会直接退出，所以每处都写一个探针文件确认。
func ensureProfile(name string) string {
	var bases []string
	if exe, err := os.Executable(); err == nil {
		bases = append(bases, filepath.Join(filepath.Dir(exe), "runtime"))
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		bases = append(bases, local)
	}
	bases = append(bases, os.TempDir())
	for _, base := range bases {
		dir := filepath.Join(base, "neomega-viewer-r2", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		probe := filepath.Join(dir, "write-probe.tmp")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			continue
		}
		_ = os.Remove(probe)
		return dir
	}
	return ""
}

// LaunchApp 用指定 ANGLE 后端打开应用窗口；angle 为空时由浏览器自己决定后端。
// 需要 Chrome 或 Edge，找不到就退回系统默认浏览器（那时无法指定后端）。
func LaunchApp(address, angle string) string {
	var browser string
	for _, candidate := range browserCandidates() {
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			browser = candidate
			break
		}
	}
	if browser == "" {
		OpenBrowser(address)
		return "系统默认浏览器（未找到 Chrome 或 Edge）"
	}
	profileName := "browser-default"
	if angle != "" {
		profileName = "browser-" + angle
	}
	args := make([]string, 0, 5)
	if angle != "" {
		args = append(args, "--use-angle="+angle)
	}
	if profile := ensureProfile(profileName); profile != "" {
		args = append(args, "--user-data-dir="+profile)
	}
	args = append(args, "--no-first-run", "--window-size=1240,840", "--app="+address)
	command := exec.Command(browser, args...)
	if err := command.Start(); err != nil {
		OpenBrowser(address)
		return fmt.Sprintf("系统默认浏览器（启动 %s 失败：%v）", filepath.Base(browser), err)
	}
	// 浏览器把请求交给已有实例后，启动进程会自己退出；这是正常交接，不必等结果。
	go func() { _ = command.Wait() }()
	backend := angle
	if backend == "" {
		backend = "浏览器默认"
	}
	return filepath.Base(browser) + "（ANGLE " + backend + "）"
}
