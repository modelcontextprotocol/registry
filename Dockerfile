FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /build/registry ./cmd/registry

FROM alpine:latest
WORKDIR /app
COPY --from=builder /build/registry .
COPY --from=builder /app/data/seed.json /app/data/seed.json
COPY --from=builder /app/internal/docs/swagger.yaml /app/internal/docs/swagger.yaml
EXPOSE 8080

CMD ["./registry"]
