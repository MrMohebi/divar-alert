FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /divar-alert .


FROM alpine:3.24
LABEL org.opencontainers.image.source="https://github.com/MrMohebi/divar-alert"

WORKDIR /root/app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=builder --chmod=755 /divar-alert ./divar-alert

ENTRYPOINT ["/root/app/divar-alert"]
