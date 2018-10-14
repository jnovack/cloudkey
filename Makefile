version := $(shell git describe --tags)
revision := $(shell git rev-parse HEAD)
release := $(shell git describe --tags | cut -d"-" -f 1,2)
build_date := $(shell date -u +"%Y-%m-%dT%H:%M:%S+00:00")
application := $(shell basename `pwd`)

GO_LDFLAGS := "-X github.com/jnovack/cloudkey/src/build.Application=${application} -X github.com/jnovack/cloudkey/src/build.Version=${version} -X github.com/jnovack/cloudkey/src/build.Revision=${revision}"

all: build

.PHONY: install
install:
	cp cloudkey.service /lib/systemd/system/cloudkey.service
	cp cloudkey /usr/local/bin/cloudkey

.PHONY: build
build:
	GOOS=linux GOARCH=arm go build -ldflags $(GO_LDFLAGS) cloudkey.go
