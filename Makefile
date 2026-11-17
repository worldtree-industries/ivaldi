# This Makefile is yours: Ivaldi wrote it once and won't touch it again.
# The shared targets (lint, test, build, run-<name>, install and setup-hooks)
# come from ivaldi.mk. Add your own targets below.

include ivaldi.mk

.PHONY: e2e

# e2e needs the network (it fetches mjolnir at latest) and, for the layer-rule
# check, golangci-lint on PATH.
e2e:
	go test -tags e2e -count=1 ./...
