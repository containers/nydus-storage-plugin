//go:build !linux

package main

// lazyUnmount is a no-op fallback on non-Linux platforms.
func lazyUnmount(_ string) error { return nil }