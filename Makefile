purged: test
	GOPATH=/usr/share/gocode go build
	strip purged

test:
	GOCACHE=/tmp GOPATH=/usr/share/gocode go test -bench . -v

# Runs an integration environment with messages produced to kafka
integration: purged
	$(CURDIR)/integration/run.sh

clean:
	-rm purged

cover:
	GOPATH=/usr/share/gocode go test -coverprofile=/tmp/coverage.out
	go tool cover -html=/tmp/coverage.out
