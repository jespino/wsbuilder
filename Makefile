.PHONY: build test install

build:
	go build -o bin/wsbuilder ./cmd/wsbuilder

test:
	go vet ./...
	go test ./...

install:
	go install ./cmd/wsbuilder
