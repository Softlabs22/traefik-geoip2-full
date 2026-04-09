//go:build !windows && !appengine && !plan9 && !js && !wasip1 && !wasi
// +build !windows,!appengine,!plan9,!js,!wasip1,!wasi

package maxminddb

func mmap(fd, length int) ([]byte, error) { return nil, nil }
func munmap(b []byte) error               { return nil }
