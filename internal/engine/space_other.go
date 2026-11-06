//go:build !linux

package engine

// Non-device builds still propagate all write errors; device builds preflight statfs.
func requireSpace(path string, needed int64) error { return nil }
