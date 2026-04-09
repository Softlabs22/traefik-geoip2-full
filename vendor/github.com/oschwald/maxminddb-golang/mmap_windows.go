//go:build windows && !appengine
// +build windows,!appengine

package maxminddb

func mmap(fd, length int) ([]byte, error) { return nil, nil }
func munmap(b []byte) error               { return nil }
