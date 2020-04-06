COMMIT_SHA := $(shell git rev-parse --short HEAD)
VERSION := '0.2.5'
.DEFAULT_GOAL := build

.PHONY: build
build:
	CGO=0 GOOS=linux go build -o remote -ldflags "-X main.CommitString=${COMMIT_SHA}" -tags "osusergo netgo static_build" cmd/main.go

.PHONY: publish
publish:
	git tag ${VERSION}
	okteto build -t okteto/remote:${VERSION} . 
