FROM golang:1.24-alpine as builder
RUN apk update && apk upgrade && apk add --no-cache bash git openssh
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o main .


FROM python:alpine3.16
RUN apk update && apk add --no-cache ffmpeg
COPY --from=builder /app/main /
CMD ["./main"]
