purged: test
	GOPATH=/usr/share/gocode go build
	strip purged

test:
	GOPATH=/usr/share/gocode go test -bench . -v

clean:
	-rm purged
