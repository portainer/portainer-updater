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
