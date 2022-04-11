dist := dist
bin := $(shell basename $(CURDIR))
image := portainer/portainer-updater

pre:
	mkdir -pv $(dist) 

build: pre
	GOOS="$(shell go env GOOS)" GOARCH="$(shell go env GOARCH)" CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s' -o $(bin)
	mv $(bin) $(dist)/

release: pre
	GOOS="$(shell go env GOOS)" GOARCH="$(shell go env GOARCH)" CGO_ENABLED=0 go build -a --installsuffix cgo --ldflags '-s' -o $(bin)
	mv $(bin) $(dist)/

image: release
	docker build -t $(image) .

clean:
	rm -rf $(dist)/*