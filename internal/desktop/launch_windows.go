//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// browserPaths 列出某种浏览器可能的安装位置，按常见程度排序。
func browserPaths(kind string) []string {
	var relative string
	switch kind {
	case "edge":
		relative = "Microsoft/Edge/Application/msedge.exe"
	case "chrome":
		relative = "Google/Chrome/Application/chrome.exe"
	default:
		return nil
	}
	var list []string
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		if base != "" {
			list = append(list, filepath.Join(base, relative))
		}
	}
	return list
}

// findBrowser 返回第一个存在的安装位置。
func findBrowser(kind string) string {
	for _, path := range browserPaths(kind) {
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path
		}
	}
	return ""
}

var versionName = regexp.MustCompile(`^\d+(\.\d+)+$`)

// browserReport 给诊断日志列出找到的浏览器、安装位置和版本（安装目录里以版本号命名的子目录）。
func browserReport() []string {
	var lines []string
	for _, kind := range []string{"edge", "chrome"} {
		path := findBrowser(kind)
		if path == "" {
			lines = append(lines, kind+" 未找到")
			continue
		}
		var versions []string
		if entries, err := os.ReadDir(filepath.Dir(path)); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && versionName.MatchString(entry.Name()) {
					versions = append(versions, entry.Name())
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s %s，版本目录 %v", kind, path, versions))
	}
	return lines
}

// osVersion 用 RtlGetVersion 取真实的 Windows 版本号，不受兼容性清单影响。
func osVersion() string {
	var info struct {
		size, major, minor, build, platform uint32
		servicePack                         [128]uint16
	}
	info.size = uint32(unsafe.Sizeof(info))
	ntdll := syscall.NewLazyDLL("ntdll.dll")
	if status, _, _ := ntdll.NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&info))); status != 0 {
		return "Windows（版本未知）"
	}
	return fmt.Sprintf("Windows %d.%d.%d", info.major, info.minor, info.build)
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

// LaunchChoice 用第 i 种组合打开应用窗口。每种组合用独立的浏览器配置目录，与日常浏览器和其他组合互不影响。
// 浏览器启动后很快以非零代码退出时，在终端提示并记进诊断日志。
func LaunchChoice(address string, i int, diag *Diagnostics) (string, error) {
	choice := Choices[i]
	browser := findBrowser(choice.Browser)
	if browser == "" {
		diag.Printf("打开 %s：未找到浏览器，查找过 %q", choice.Name, browserPaths(choice.Browser))
		return "", fmt.Errorf("没有找到 %s", choice.Browser)
	}
	var args []string
	if choice.Angle != "" {
		args = append(args, "--use-angle="+choice.Angle)
	}
	if profile := ensureProfile("browser-" + choice.Key); profile != "" {
		args = append(args, "--user-data-dir="+profile)
	} else {
		diag.Printf("打开 %s：没有可写的浏览器配置目录，使用浏览器默认配置", choice.Name)
	}
	args = append(args, "--no-first-run", "--window-size=1240,840", "--app="+address)
	diag.Printf("打开 %s：%s %q", choice.Name, browser, args)
	command := exec.Command(browser, args...)
	if err := command.Start(); err != nil {
		diag.Printf("打开 %s 失败：%v", choice.Name, err)
		return "", err
	}
	started := time.Now()
	// 浏览器把请求交给已有实例后，启动进程会自己以 0 退出；这是正常交接。
	go func() {
		err := command.Wait()
		elapsed := time.Since(started).Seconds()
		code := command.ProcessState.ExitCode()
		diag.Printf("%s 的启动进程退出：代码 %d，运行 %.1f 秒，%v", choice.Name, code, elapsed, err)
		if code != 0 && elapsed < 20 {
			fmt.Printf("  %s 启动后很快退出（代码 %d），可以点别的按钮换一种组合。\n", choice.Name, code)
		}
	}()
	return choice.Name, nil
}

// RestrictedLocation 检查 exe 所在目录及其上级是否显式设置了拒绝项或低完整性标签。
// 开发工具的沙箱会这样保护工作区；从这类目录里的程序启动浏览器时，实测 Chrome 启动即崩溃、Edge 一直白屏，
// 浏览器配置放在别处也一样，只有把程序本身移出去才正常。
// 返回带特殊权限的目录及其 SDDL，没有时 dir 为空。
func RestrictedLocation() (dir, sddl string) {
	exe, err := os.Executable()
	if err != nil {
		return "", ""
	}
	for path := filepath.Dir(exe); ; path = filepath.Dir(path) {
		if text := securityDescriptor(path); text != "" && explicitRestriction(text) {
			return path, text
		}
		if filepath.Dir(path) == path {
			return "", ""
		}
	}
}

// explicitRestriction 判断 SDDL 里是否有非继承（不带 ID 标志）的拒绝项或低完整性标签。
func explicitRestriction(sddl string) bool {
	for _, ace := range strings.Split(sddl, "(")[1:] {
		fields := strings.Split(strings.TrimSuffix(strings.SplitN(ace, ")", 2)[0], ")"), ";")
		if len(fields) < 2 || strings.Contains(fields[1], "ID") {
			continue
		}
		if fields[0] == "D" || (fields[0] == "ML" && len(fields) >= 6 && fields[5] == "LW") {
			return true
		}
	}
	return false
}

// securityDescriptor 读取目录的访问控制列表与完整性标签，转成 SDDL 文本；失败时返回空。
func securityDescriptor(path string) string {
	const (
		fileObject       = 1
		daclInformation  = 0x4
		labelInformation = 0x10
		sddlRevision     = 1
	)
	advapi := syscall.NewLazyDLL("advapi32.dll")
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var descriptor uintptr
	if r, _, _ := advapi.NewProc("GetNamedSecurityInfoW").Call(uintptr(unsafe.Pointer(name)), fileObject,
		daclInformation|labelInformation, 0, 0, 0, 0, uintptr(unsafe.Pointer(&descriptor))); r != 0 {
		return ""
	}
	defer syscall.LocalFree(syscall.Handle(descriptor))
	var text *uint16
	var length uint32
	if r, _, _ := advapi.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW").Call(descriptor, sddlRevision,
		daclInformation|labelInformation, uintptr(unsafe.Pointer(&text)), uintptr(unsafe.Pointer(&length))); r == 0 {
		return ""
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(text)))
	return syscall.UTF16ToString(unsafe.Slice(text, length))
}
