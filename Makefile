.PHONY: pre build release image clean

dist := dist
image_release := portainer/portainer-updater
image_dev := portainerci/portainer-updater
tag ?= latest

ifeq ("$(PLATFORM)", "windows")
bin=portainer-updater.exe
else
bin=portainer-updater
endif

PLATFORM?=$(shell go env GOOS)
ARCH?=$(shell go env GOARCH)

pre:
	mkdir -pv $(dist) 

build: pre
	GOOS="$(PLATFORM)" GOARCH="$(ARCH)" CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s' -o $(dist)/$(bin)

release: pre
	GOOS="$(PLATFORM)" GOARCH="$(ARCH)" CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s' -o $(dist)/$(bin)

image: build
	docker build -t $(image_dev):$(tag) .

clean:
	rm -rf $(dist)/*