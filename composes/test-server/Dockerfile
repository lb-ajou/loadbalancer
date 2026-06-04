FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/test-server ./main.go

FROM alpine:3.20

RUN apk add --no-cache ca-certificates wget

WORKDIR /app
COPY --from=builder /out/test-server /app/test-server

EXPOSE 8080

ENTRYPOINT ["/app/test-server"]
