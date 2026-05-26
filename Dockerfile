FROM golang:1.26.3-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/httpserverd ./cmd/httpserverd

FROM alpine:3.22

RUN addgroup -S httpserver && adduser -S -G httpserver httpserver \
	&& apk add --no-cache ca-certificates wget

WORKDIR /app

COPY --from=build /out/httpserverd /usr/local/bin/httpserverd
COPY deploy/production.env.example /app/production.env.example

USER httpserver

ENV HTTP_SERVER_ADDR=:8080
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD wget -q -O - http://127.0.0.1:8080/healthz/live >/dev/null || exit 1

ENTRYPOINT ["/usr/local/bin/httpserverd"]
