FROM golang:1.24-alpine AS builder
WORKDIR /app

# Copy Paychex root certificate FIRST (before go mod download needs it)
COPY certs/paychex-root.pem /usr/local/share/ca-certificates/paychex-root.crt

# Install ca-certificates and update certificate store
# Note: ca-certificates package is already in the base golang:alpine image
RUN cat /usr/local/share/ca-certificates/paychex-root.crt >> /etc/ssl/certs/ca-certificates.crt && \
    update-ca-certificates

# Copy go mod files first and download dependencies
# This creates a separate layer that only invalidates when dependencies change
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

ARG GO_BUILD_TAGS
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN go build \
    ${GO_BUILD_TAGS:+-tags="$GO_BUILD_TAGS"} \
    -ldflags="-X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT} -X main.BuildTime=${BUILD_TIME}" \
    -o /build/registry ./cmd/registry

FROM alpine:latest
WORKDIR /app

# Copy Paychex root certificate FIRST (before apk can use it)
COPY certs/paychex-root.pem /usr/local/share/ca-certificates/paychex-root.crt

# Manually add the certificate to the bundle (ca-certificates not installed yet)
RUN cat /usr/local/share/ca-certificates/paychex-root.crt >> /etc/ssl/certs/ca-certificates.crt

# Now we can install ca-certificates package and update properly
RUN apk add --no-cache ca-certificates && \
    update-ca-certificates

COPY --from=builder /build/registry .
COPY --from=builder /app/data/seed.json /app/data/seed.json

# Create a non-privileged user that the app will run under.
# See https://docs.docker.com/go/dockerfile-user-best-practices/
ARG UID=10001
RUN adduser \
    --disabled-password \
    --gecos "" \
    --home "/nonexistent" \
    --shell "/sbin/nologin" \
    --no-create-home \
    --uid "${UID}" \
    appuser

USER appuser
EXPOSE 8080

ENTRYPOINT ["./registry"]
