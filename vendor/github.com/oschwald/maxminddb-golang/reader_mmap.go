//go:build !appengine && !plan9 && !js && !wasip1 && !wasi
// +build !appengine,!plan9,!js,!wasip1,!wasi

package maxminddb

import "os"

func Open(file string) (*Reader, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return FromBytes(data)
}

func (r *Reader) Close() error {
	r.buffer = nil
	return nil
}
