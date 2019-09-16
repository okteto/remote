.DEFAULT_GOAL := build

.PHONY: build
build:
	CGO=0 GOOS=linux go build -o remote -tags "osusergo netgo static_build" ./...