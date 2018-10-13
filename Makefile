version := $(shell git describe --tags)
revision := $(shell git rev-parse HEAD)
release := $(shell git describe --tags | cut -d"-" -f 1,2)
build_date := $(shell date -Iseconds --utc)

GO_LDFLAGS := "-X main.Version=${version} -X main.Revision=${revision}"

all: build

.PHONY: install
install:
	cp cloudkey.service /lib/systemd/system/cloudkey.service
	cp cloudkey /usr/local/bin/cloudkey

.PHONY: build
build:
	GOOS=linux GOARCH=arm go build -ldflags $(GO_LDFLAGS) cloudkey.go
