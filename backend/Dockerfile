FROM golang:1.22-alpine AS builder
WORKDIR /src
RUN apk add --no-cache ca-certificates git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/concurrency-test ./cmd/concurrency-test

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget && addgroup -S cinema && adduser -S -G cinema cinema
WORKDIR /app
COPY --from=builder /out/ /app/
COPY docs/ /app/docs/
USER cinema
EXPOSE 8080
ENTRYPOINT ["/app/api"]
