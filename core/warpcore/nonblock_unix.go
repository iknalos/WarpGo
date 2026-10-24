//go:build !windows

package warpcore

import "syscall"

func setNonblock(fd int) error { return syscall.SetNonblock(fd, true) }
