version := $(shell git describe --tags 2>/dev/null || echo dev)
revision := $(shell git rev-parse HEAD 2>/dev/null || echo local)
release := $(shell git describe --tags 2>/dev/null | cut -d"-" -f 1,2)
build_date := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
application := $(shell basename `pwd`)

GO_LDFLAGS := "-X github.com/jnovack/cloudkey/internal/buildversion.Version=${version} -X github.com/jnovack/cloudkey/internal/buildversion.Revision=${revision} -X github.com/jnovack/cloudkey/internal/buildversion.BuildRFC3339=${build_date}"

# WEB_ROOT must match the cloudkey -web-root default so the deployed dashboard
# is found without extra configuration.
WEB_ROOT := /usr/share/cloudkey/website

all: build

# First-time setup of a remote device. `deploy` only updates an already-running
# install (it restarts the unit), so a fresh Cloud Key needs this once first.
# Idempotent, so it is also the way to push a changed cloudkey.service.
.PHONY: install
install:
	scp -i $(DEPLOY_KEY) cloudkey.service $(DEPLOY_HOST):/lib/systemd/system/cloudkey.service
	ssh -i $(DEPLOY_KEY) $(DEPLOY_HOST) 'touch /etc/cloudkey.env && mkdir -p $(WEB_ROOT) && systemctl daemon-reload && systemctl enable cloudkey'
	$(MAKE) deploy

.PHONY: build
build:
	mkdir -p .local/bin
	GOOS=linux GOARCH=arm go build -ldflags $(GO_LDFLAGS) -o .local/bin/cloudkey ./cmd/cloudkey

.PHONY: deploy
deploy: build
	scp -i $(DEPLOY_KEY) .local/bin/cloudkey $(DEPLOY_HOST):/tmp/cloudkey.new
	ssh -i $(DEPLOY_KEY) $(DEPLOY_HOST) 'mkdir -p $(WEB_ROOT)'
	scp -i $(DEPLOY_KEY) website/dashboard.html $(DEPLOY_HOST):$(WEB_ROOT)/
	ssh -i $(DEPLOY_KEY) $(DEPLOY_HOST) 'chmod 755 /tmp/cloudkey.new && mv /tmp/cloudkey.new /usr/local/bin/cloudkey && systemctl restart cloudkey && systemctl status cloudkey --no-pager'

.PHONY: preview
preview:
	mkdir -p .local/preview
	CLOUDKEY_PREVIEW_DIR=$(CURDIR)/.local/preview go test ./internal/display -run TestPreviewScreens -v
