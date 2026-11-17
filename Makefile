# This Makefile is yours: Ivaldi wrote it once and won't touch it again.
# The shared targets (lint, test, build, run-<name> and install) come from
# ivaldi.mk. Add your own targets below.

include ivaldi.mk

.PHONY: e2e setup-hooks

# setup-hooks turns on .githooks/pre-commit (make lint test) for this clone.
setup-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit

# e2e needs the network (it fetches mjolnir at latest) and, for the layer-rule
# check, golangci-lint on PATH.
e2e:
	go test -tags e2e -count=1 ./...
