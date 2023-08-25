SHELL:=/usr/bin/env bash
VERSION:=$(shell [ -z "$$(git tag --points-at HEAD)" ] && echo "$$(git describe --always --long --dirty | sed 's/^v//')" || echo "$$(git tag --points-at HEAD | sed 's/^v//')")
GO_FILES:=$(shell find . -name '*.go' | grep -v /vendor/)
BIN_NAME:=openweather-influxdb-connector

default: help

# via https://marmelab.com/blog/2016/02/29/auto-documented-makefile.html
.PHONY: help
help: ## Print help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: clean
clean: ## Remove built products in ./out
	rm -rf ./out

.PHONY: lint
lint: ## Lint all .go files
	golangci-lint run *.go

.PHONY: build
build: lint ## Build (for the current platform & architecture) to ./out
	mkdir -p out
	go build -ldflags="-X main.version=${VERSION}" -o ./out/${BIN_NAME} .

.PHONY: build-linux-amd64
build-linux-amd64: lint ## Build for Linux/amd64 to ./out
	mkdir -p out/linux-amd64
	env GOOS=linux GOARCH=amd64 go build -ldflags="-X main.version=${VERSION}" -o ./out/linux-amd64/${BIN_NAME} .