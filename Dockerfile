FROM --platform=$BUILDPLATFORM golang:1.26.3-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY main.go ./
COPY configs ./configs
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/loadbalancer ./main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget

WORKDIR /app
COPY --from=builder /out/loadbalancer /app/loadbalancer
COPY configs /app/configs

EXPOSE 8080 9090

ENTRYPOINT ["/app/loadbalancer"]
