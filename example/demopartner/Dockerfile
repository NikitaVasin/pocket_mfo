FROM golang:1.27.0-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY example/demopartner ./example/demopartner
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/demo-partner ./example/demopartner

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S partner && adduser -S -G partner partner && \
    mkdir /data && chown partner:partner /data
COPY --from=build /out/demo-partner /usr/local/bin/demo-partner
USER partner
ENV PARTNER_ADDR=0.0.0.0:8091 PARTNER_DATA=/data/partner.json
VOLUME /data
EXPOSE 8091
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s \
    CMD wget -q -O /dev/null http://127.0.0.1:8091/api/health || exit 1
ENTRYPOINT ["demo-partner"]
