.PHONY: pre build release image clean

dist := dist
image_release := portainer/portainer-updater
image_dev := portainerci/portainer-updater
tag ?= latest
dockerfile := "build/linux/Dockerfile"

ifeq ("$(PLATFORM)", "windows")
bin=portainer-updater.exe
else
bin=portainer-updater
endif

PLATFORM?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)
GIT_COMMIT?=$(shell git log -1 --format=%h)
GOTESTSUM=go run gotest.tools/gotestsum@latest
GOLANGCI_LINT_VERSION := $(shell cat $(shell git rev-parse --show-toplevel)/.golangci-version)

pre:
	mkdir -pv $(dist) 

build: pre
	GOOS="$(PLATFORM)" GOARCH="$(ARCH)" CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s' -o $(dist)/$(bin)

release: pre
	GOOS="$(PLATFORM)" GOARCH="$(ARCH)" CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s' -o $(dist)/$(bin)

image: build
	docker build -f $(dockerfile) -t $(image_dev):$(tag) --build-arg GIT_COMMIT=$(GIT_COMMIT) .

image_release: release
	docker build -f $(dockerfile) -t $(image_release):$(tag) --build-arg GIT_COMMIT=$(GIT_COMMIT) .

tidy: 
	go mod tidy

clean:
	rm -rf $(dist)/*

check-lint-version:
	@installed=v$$(golangci-lint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$installed" = "v" ]; then \
		echo "ERROR: golangci-lint not found, need $(GOLANGCI_LINT_VERSION)"; \
		echo "Install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	elif [ "$$installed" != "$(GOLANGCI_LINT_VERSION)" ]; then \
		echo "ERROR: golangci-lint $$installed installed, need $(GOLANGCI_LINT_VERSION)"; \
		echo "Install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi

lint: check-lint-version
	golangci-lint run --timeout=10m -c .golangci.yaml

test:
	$(GOTESTSUM) --format pkgname-and-test-fails --format-hide-empty-pkg --hide-summary skipped -- -cover -covermode=atomic -coverprofile=coverage.out ./...

format:
	go fmt ./...
