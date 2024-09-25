ARG BASE_IMAGE=portainer/base
ARG GIT_COMMIT=unspecified
ARG GO_VERSION=1.22.6

# Stage 1: Build the Go application
FROM golang:${GO_VERSION}-alpine3.20 AS build

# Set the working directory inside the container
WORKDIR /app

# Copy the rest of the source code
COPY --link . .

# Download Go module dependencies
RUN go mod download -x

# Build the Go application
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build --installsuffix cgo --ldflags '-s' -o portainer-updater

# Stage 2: Create the final image
FROM ${BASE_IMAGE}

# Set the GIT_COMMIT label
ARG GIT_COMMIT
LABEL git_commit=$GIT_COMMIT

# Copy the compiled binary from the build stage
COPY --from=build /app/portainer-updater /portainer-updater

# Set the entrypoint
ENTRYPOINT [ "/portainer-updater" ]
