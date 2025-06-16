FROM golang:1.24 AS build
WORKDIR /void/repo-exporter
COPY . .
RUN go mod vendor && \
        GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o /exporter .

FROM alpine:latest AS certs
RUN apk --update add ca-certificates

FROM scratch
COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /exporter /exporter
LABEL org.opencontainers.image.source=https://github.com/void-linux/repo-exporter
ENTRYPOINT ["/exporter"]
