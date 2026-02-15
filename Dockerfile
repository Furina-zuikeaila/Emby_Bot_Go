# syntax=docker/dockerfile:1

FROM golang:1.21-alpine AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
	go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 app

# 预创建持久化目录（docker-compose 将挂载 ./data 到 /app/data）。
RUN mkdir -p /app/data && chown -R app:app /app

USER app
WORKDIR /app

COPY --from=builder /out/bot /app/bot

EXPOSE 8900

ENTRYPOINT ["/app/bot"]
