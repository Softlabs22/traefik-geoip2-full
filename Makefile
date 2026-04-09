.PHONY: default test yaegi_test vendor vendor-patch clean

export GO111MODULE=on

default: test

test:
	go test -v -cover ./...

yaegi_test:
	yaegi test -v .

vendor:
	go mod tidy
	go mod vendor
	make vendor-patch

# Re-apply Yaegi compatibility patches after "go mod vendor".
# Required because mmap in maxminddb-golang uses unsafe/syscall which Yaegi cannot interpret.
vendor-patch:
	@echo "Applying Yaegi compatibility patch to maxminddb-golang..."
	printf '%s\n' \
		'//go:build !windows && !appengine && !plan9 && !js && !wasip1 && !wasi' \
		'// +build !windows,!appengine,!plan9,!js,!wasip1,!wasi' \
		'' \
		'package maxminddb' \
		'' \
		'func mmap(fd, length int) ([]byte, error) { return nil, nil }' \
		'func munmap(b []byte) error               { return nil }' \
		> vendor/github.com/oschwald/maxminddb-golang/mmap_unix.go
	printf '%s\n' \
		'//go:build windows && !appengine' \
		'// +build windows,!appengine' \
		'' \
		'package maxminddb' \
		'' \
		'func mmap(fd, length int) ([]byte, error) { return nil, nil }' \
		'func munmap(b []byte) error               { return nil }' \
		> vendor/github.com/oschwald/maxminddb-golang/mmap_windows.go
	printf '%s\n' \
		'//go:build !appengine && !plan9 && !js && !wasip1 && !wasi' \
		'// +build !appengine,!plan9,!js,!wasip1,!wasi' \
		'' \
		'package maxminddb' \
		'' \
		'import "os"' \
		'' \
		'func Open(file string) (*Reader, error) {' \
		'	data, err := os.ReadFile(file)' \
		'	if err != nil {' \
		'		return nil, err' \
		'	}' \
		'	return FromBytes(data)' \
		'}' \
		'' \
		'func (r *Reader) Close() error {' \
		'	r.buffer = nil' \
		'	return nil' \
		'}' \
		> vendor/github.com/oschwald/maxminddb-golang/reader_mmap.go
	rm -rf vendor/golang.org
	printf '%s\n' \
		'# github.com/oschwald/geoip2-golang v1.11.0' \
		'## explicit; go 1.21' \
		'github.com/oschwald/geoip2-golang' \
		'# github.com/oschwald/maxminddb-golang v1.13.0' \
		'## explicit; go 1.21' \
		'github.com/oschwald/maxminddb-golang' \
		'# golang.org/x/sys v0.20.0' \
		'## explicit; go 1.18' \
		> vendor/modules.txt
	@echo "Done."

clean:
	rm -rf ./vendor
