.PHONY: build test

build:
	go build -o scalefleet ./cmd/scalefleet

test:
	go test ./...
