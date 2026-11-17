# Managed by Ivaldi. This file is the same in every Project and is kept
# current for you, so don't edit it: add your own targets to Makefile.
#
# Binaries are discovered from cmd/ each time make runs:
#   lint          run golangci-lint
#   test          run the tests
#   build         build everything, and each binary into bin/
#   run-<name>    run cmd/<name>
#   install       go install every binary

BINARIES := $(sort $(patsubst cmd/%/,%,$(dir $(wildcard cmd/*/main.go))))

.DEFAULT_GOAL := build
.PHONY: lint test build install

lint:
	golangci-lint run ./...

test:
	go test ./...

build:
	go build ./...
ifneq ($(BINARIES),)
	go build -o bin/ $(addprefix ./cmd/,$(BINARIES))
endif

run-%:
	go run ./cmd/$*

install:
ifneq ($(BINARIES),)
	go install $(addprefix ./cmd/,$(BINARIES))
else
	@echo "No binaries in cmd/ to install."
endif
