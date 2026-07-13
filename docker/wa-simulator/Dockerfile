FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY main.go .
RUN go mod init wa-simulator && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /wa-simulator .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /wa-simulator /wa-simulator
EXPOSE 9000
ENTRYPOINT ["/wa-simulator"]
