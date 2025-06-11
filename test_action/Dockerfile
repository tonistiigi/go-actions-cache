FROM golang:alpine
RUN apk add --no-cache openssl
ENV CGO_ENABLED=0
ENV ACTIONS_CACHE_API_FORCE_VERSION=v2
WORKDIR /github/workspace
ENTRYPOINT [ "go", "test", "-v" ]
