.PHONY: run build test test-go test-js test-cover clean

run:
	go run .

build:
	go build -trimpath -ldflags="-s -w" -o goclipboard .

# Runs both the Go suite and the browser-CRDT cross-check (needs node).
test: test-go test-js

test-go:
	go test ./...

test-js:
	node static/crdt.test.js

test-cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "coverage report: coverage.html"

clean:
	rm -f goclipboard coverage.out coverage.html
