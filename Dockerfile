FROM golang:1.24-alpine AS builder

WORKDIR /root/go/
COPY . .
RUN apk --no-cache add make git gcc libtool musl-dev ca-certificates dumb-init curl
RUN go get .
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o divar-alert .


FROM alpine:3.17
LABEL org.opencontainers.image.source="https://github.com/MrMohebi/stremio-ir-providers"

WORKDIR /root/app

COPY --from=builder --chmod=777 /root/go/divar-alert ./divar-alert

ENTRYPOINT ["/root/app/divar-alert"]
