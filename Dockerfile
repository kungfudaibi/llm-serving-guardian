# syntax=docker/dockerfile:1
FROM golang:1.26.5-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/guardian ./cmd/guardian && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock-worker ./cmd/mock-worker

FROM alpine:3.24.1 AS mock-worker
RUN addgroup -S guardian && adduser -S -G guardian guardian
COPY --from=build /out/mock-worker /usr/local/bin/mock-worker
USER guardian
EXPOSE 8082
ENTRYPOINT ["mock-worker"]

FROM alpine:3.24.1 AS guardian
RUN addgroup -S guardian && adduser -S -G guardian guardian
COPY --from=build /out/guardian /usr/local/bin/guardian
COPY configs/docker.json /etc/guardian/config.json
USER guardian
EXPOSE 8090
ENTRYPOINT ["guardian", "-config", "/etc/guardian/config.json"]
