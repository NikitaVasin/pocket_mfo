FROM golang:1.27.0-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY plugins ./plugins
COPY example ./example
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pocketbase ./example

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S pocketbase && adduser -S -G pocketbase pocketbase && \
    mkdir /pb_data && chown pocketbase:pocketbase /pb_data
COPY --from=build /out/pocketbase /usr/local/bin/pocketbase
USER pocketbase
VOLUME /pb_data
EXPOSE 8090
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s \
    CMD wget -q -O /dev/null http://127.0.0.1:8090/api/health || exit 1
ENTRYPOINT ["pocketbase"]
CMD ["serve", "--http=0.0.0.0:8090", "--dir=/pb_data"]
