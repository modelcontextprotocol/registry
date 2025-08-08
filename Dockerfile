FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
ARG GO_BUILD_TAGS
RUN go build ${GO_BUILD_TAGS:+-tags="$GO_BUILD_TAGS"} -o /build/registry ./cmd/registry

FROM alpine:latest
WORKDIR /app
COPY --from=builder /build/registry .
COPY --from=builder /app/data/seed.json /app/data/seed.json
COPY --from=builder /app/internal/docs/swagger.yaml /app/internal/docs/swagger.yaml

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
