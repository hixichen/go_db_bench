GO ?= go
GOVERSIONS ?= go1.11 go1.12

all: vendor fmt vet build test

GOPATH = $(shell go env GOPATH)

GOPATH = $(shell go env GOPATH)

vendor:
	@echo "=== go mod vendor ==="
	go mod vendor

fmt:
	@echo "=== go fmt ==="
	go fmt ./...

vet:
	@echo "==== go vet ==="
	go vet ./...

build: vendor
	go build -o bin/db_bench

test: vendor
	go test -test.bench=. -test.benchmem

help:
	@$(MAKE) -pRrq -f $(lastword $(MAKEFILE_LIST)) : 2>/dev/null | awk -v RS= -F: '/^# File/,/^# Finished Make data base/ {if ($$1 !~ "^[#.]") {print $$1}}' | sort | egrep -v -e '^[^[:alnum:]]' -e '^$@$$' | xargs
