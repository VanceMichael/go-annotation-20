GOTOOLCHAIN ?= local
export GOTOOLCHAIN

.PHONY: build test race vet fmt selfcheck docker clean

build:
	go build -o bin/floodctl ./cmd/floodctl

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

selfcheck: build
	./bin/floodctl selfcheck

docker:
	docker build -t floodwatch:local .

clean:
	rm -rf bin
