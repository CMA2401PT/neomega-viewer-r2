//go:build !windows

package filebrowse

import (
	"context"
	"errors"
)

func NativeDropAvailable() bool { return false }

func NativeDrop(context.Context) (string, error) {
	return "", errors.New("native file drop is unavailable")
}
