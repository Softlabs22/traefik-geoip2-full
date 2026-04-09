.PHONY: default test yaegi_test vendor clean

export GO111MODULE=on

default: test

test: testdata/GeoIP2-City-Test.mmdb
	go test -v -cover ./...

yaegi_test:
	yaegi test -v .

testdata/GeoIP2-City-Test.mmdb:
	mkdir -p testdata
	curl -sSL -o testdata/GeoIP2-City-Test.mmdb \
		https://github.com/maxmind/MaxMind-DB/raw/main/test-data/GeoIP2-City-Test.mmdb

vendor:
	go mod vendor

clean:
	rm -rf ./vendor
