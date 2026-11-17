//go:build windows

package desktop

import (
	"encoding/binary"
	"os"
	"syscall"
	"unsafe"
)

const (
	enableProcessedInput = 0x0001
	enableMouseInput     = 0x0010
	enableExtendedFlags  = 0x0080
	keyEvent             = 0x0001
	mouseEvent           = 0x0002
	leftButton           = 0x0001
	doubleClick          = 0x0002
)

var (
	kernel32                    = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode          = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode          = kernel32.NewProc("SetConsoleMode")
	procReadConsoleInput        = kernel32.NewProc("ReadConsoleInputW")
	procReadConsoleOutputChar   = kernel32.NewProc("ReadConsoleOutputCharacterW")
	procGetConsoleScreenBufInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

// inputRecord 对应 INPUT_RECORD：事件类型后接 16 字节的联合体。
type inputRecord struct {
	kind uint16
	_    uint16
	data [16]byte
}

// ListenChoices 让控制台接收鼠标点击和数字键，点中按钮或按 1～3 时调用 pick。
// 关闭快速编辑模式，否则单击会变成选择文字；返回恢复原输入模式的函数。输入不是控制台时 ok 为假。
func ListenChoices(pick func(int)) (restore func(), ok bool) {
	input := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(uintptr(input), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return func() {}, false
	}
	if r, _, _ := procSetConsoleMode.Call(uintptr(input), enableProcessedInput|enableMouseInput|enableExtendedFlags); r == 0 {
		return func() {}, false
	}
	go func() {
		var record inputRecord
		var read uint32
		for {
			r, _, _ := procReadConsoleInput.Call(uintptr(input), uintptr(unsafe.Pointer(&record)), 1, uintptr(unsafe.Pointer(&read)))
			if r == 0 {
				return
			}
			if read == 0 {
				continue
			}
			switch record.kind {
			case keyEvent:
				down := binary.LittleEndian.Uint32(record.data[0:])
				char := binary.LittleEndian.Uint16(record.data[10:])
				if down != 0 && char >= '1' && int(char) < '1'+len(Choices) {
					pick(int(char - '1'))
				}
			case mouseEvent:
				x := int16(binary.LittleEndian.Uint16(record.data[0:]))
				y := int16(binary.LittleEndian.Uint16(record.data[2:]))
				buttons := binary.LittleEndian.Uint32(record.data[4:])
				flags := binary.LittleEndian.Uint32(record.data[12:])
				if buttons&leftButton == 0 || (flags != 0 && flags != doubleClick) {
					continue
				}
				if i := hitButton(consoleRow(y), int(x)); i >= 0 {
					pick(i)
				}
			}
		}
	}()
	return func() { procSetConsoleMode.Call(uintptr(input), uintptr(mode)) }, true
}

// consoleRow 读回控制台缓冲区第 y 行的文字。按钮行之后终端可能继续滚动，所以每次点击时按行内容判断，而不是记住按钮的坐标。
func consoleRow(y int16) string {
	output := syscall.Handle(os.Stdout.Fd())
	var info [22]byte
	if r, _, _ := procGetConsoleScreenBufInfo.Call(uintptr(output), uintptr(unsafe.Pointer(&info[0]))); r == 0 {
		return ""
	}
	width := int(int16(binary.LittleEndian.Uint16(info[0:])))
	if width <= 0 || y < 0 {
		return ""
	}
	cells := make([]uint16, width)
	var read uint32
	coord := uint32(uint16(y)) << 16
	if r, _, _ := procReadConsoleOutputChar.Call(uintptr(output), uintptr(unsafe.Pointer(&cells[0])), uintptr(width), uintptr(coord), uintptr(unsafe.Pointer(&read))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(cells[:read])
}
