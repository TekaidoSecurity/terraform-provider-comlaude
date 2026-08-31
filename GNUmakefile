default: fmt lint install generate

build:
	go build -v ./...

install: build
	go install -v ./...

lint:
	golangci-lint run

generate:
	cd tools; go generate ./...

fmt:
	gofmt -s -w -e .

test:
	go test -v -cover -timeout=120s -parallel=10 ./...

# Acceptance tests run against the LIVE Comlaude API, scoped to the
# designated test domain. Credentials are loaded (never sourced) from
# ~/.config/comlaude/env when not already in the environment.
testacc:
	bash scripts/testacc.sh

.PHONY: fmt lint test testacc build install generate
