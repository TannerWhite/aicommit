.PHONY: build test lint smoke
build:
	go build -o bin/aicommit ./cmd/aicommit
test:
	go test -race ./...
lint:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
smoke: build
	cd "$$(mktemp -d)" && git init -q && echo hi > a.txt && $(CURDIR)/bin/aicommit --dry-run
