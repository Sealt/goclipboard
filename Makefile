.PHONY: run build test test-js test-cover clean

run:
	go run .

build:
	go build -trimpath -ldflags="-s -w" -o goclipboard .

test:
	go test ./...

test-js:
	node static/crdt.test.js

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

clean:
	rm -f goclipboard coverage.out coverage.html
