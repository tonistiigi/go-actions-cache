FROM golang:alpine
RUN apk add --no-cache openssl
ENV CGO_ENABLED=0
WORKDIR /github/workspace
ENTRYPOINT [ "go", "test", "-v" ]