//go:build linux

package main

import "golang.org/x/sys/unix"

// lazyUnmount performs a lazy unmount (MNT_DETACH) on Linux.
func lazyUnmount(target string) error {
	return unix.Unmount(target, unix.MNT_DETACH)
}