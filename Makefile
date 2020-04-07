purged:
	GOPATH=/usr/share/gocode go build
	strip purged

clean:
	-rm purged
