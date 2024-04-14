# syntax=docker/dockerfile:1.4

ARG GO_VERSION="1.22.1"

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH

ENV GOOS=${TARGETOS}
ENV GOARCH=${TARGETARCH}

WORKDIR /app

# Leverage caching by copying only the necessary files for dependency download
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy the rest of the source code
COPY . .

# Compile the static binary with optimizations for a smaller binary size
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -ldflags '-w -s' -trimpath -o /go/bin/app

# Use a Distroless base image for the final stage to reduce the attack surface and image size
FROM gcr.io/distroless/static-debian11

# Copy the pre-built binary file from the previous stage
COPY --from=builder /go/bin/app /

# Correct the CMD path to match the location where the binary is copied
ENTRYPOINT ["/app"]
