FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
ARG GO_BUILD_TAGS
RUN go build ${GO_BUILD_TAGS:+-tags="$GO_BUILD_TAGS"} -o /build/registry ./cmd/registry

FROM alpine:latest
WORKDIR /app
COPY --from=builder /build/registry .
COPY --from=builder /app/data/seed.json /app/data/seed.json
COPY --from=builder /app/internal/docs/swagger.yaml /app/internal/docs/swagger.yaml
EXPOSE 8080

ENTRYPOINT ["./registry"]
