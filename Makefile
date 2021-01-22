COMMIT_SHA ?= $(shell git rev-parse --short HEAD)
.DEFAULT_GOAL := build

.PHONY: build test

build:
	CGO=0 go build -o remote -ldflags "-X main.CommitString=${COMMIT_SHA}" -tags "osusergo netgo static_build" cmd/main.go

test:
	 go test -p 4 -coverprofile=coverage.txt -covermode=atomic ./...