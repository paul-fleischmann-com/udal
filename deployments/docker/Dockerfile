FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN cd gateway && go mod download && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /udal-gateway ./cmd/gateway/

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /udal-gateway /udal-gateway
EXPOSE 50051 8080
ENTRYPOINT ["/udal-gateway"]
