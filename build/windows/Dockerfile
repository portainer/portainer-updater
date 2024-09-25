ARG OSVERSION=1809
ARG GIT_COMMIT=unspecified
ARG GO_VERSION=1.22.6
ARG GIT_VERSION=2.46.0
ARG GIT_PATCH_VERSION=1

# Stage 1: Download Git and prepare the environment
FROM --platform=linux/amd64 alpine:3.20 AS downloader
ARG GIT_VERSION
ARG GIT_PATCH_VERSION

# Download and extract MinGit for Windows
RUN mkdir mingit/ \
  && wget https://github.com/git-for-windows/git/releases/download/v${GIT_VERSION}.windows.${GIT_PATCH_VERSION}/MinGit-${GIT_VERSION}-busybox-64-bit.zip \
  && unzip MinGit-${GIT_VERSION}-busybox-64-bit.zip -d mingit/

FROM --platform=linux/amd64 gcr.io/k8s-staging-e2e-test-images/windows-servercore-cache:1.0-linux-amd64-${OSVERSION} AS core

# Stage 2: Build the Go application
FROM --platform=linux/amd64 golang:${GO_VERSION}-alpine3.20 AS build

# Set the working directory inside the container
WORKDIR /go/src/app

# Copy the rest of the source code
COPY --link . .

# Build the Go application with required flags
ARG GIT_COMMIT
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s' -o portainer-updater.exe

# Stage 3: Create the final image
FROM mcr.microsoft.com/windows/nanoserver:${OSVERSION}
ARG GIT_COMMIT

# Set the PATH
ENV PATH="C:\mingit\cmd;C:\app;C:\Windows\system32;C:\Windows;"

# Copy the Git tools and built application from previous stages
COPY --from=downloader /mingit mingit/
COPY --from=core C:/Windows/System32/netapi32.dll C:/Windows/System32/netapi32.dll
COPY --from=build /go/src/app/portainer-updater.exe C:/app/portainer-updater.exe

# Set user as ContainerAdministrator for appropriate permissions
USER ContainerAdministrator

# Set the GIT_COMMIT label
LABEL git_commit=$GIT_COMMIT

# Set the entrypoint to run the built application
ENTRYPOINT [ "C:/app/portainer-updater.exe" ]