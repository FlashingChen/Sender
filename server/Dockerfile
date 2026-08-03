FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/sender-server ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache tzdata \
    && addgroup -S sender && adduser -S -G sender sender \
    && mkdir -p /data \
    && chown -R sender:sender /data
COPY --from=build /out/sender-server /usr/local/bin/sender-server

ENV ADDR=:8080 \
    DB_PATH=/data/messages.db \
    TZ=Asia/Shanghai
EXPOSE 8080
VOLUME ["/data"]
USER sender
ENTRYPOINT ["/usr/local/bin/sender-server"]
