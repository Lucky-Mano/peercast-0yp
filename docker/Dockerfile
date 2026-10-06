FROM node:lts-alpine AS web
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /build/internal/httpd/dist/web ./internal/httpd/dist/web
RUN CGO_ENABLED=0 go build -o peercast-0yp .

FROM alpine:3.21
RUN apk add --no-cache tzdata
WORKDIR /app
COPY --from=builder /build/peercast-0yp .
ENTRYPOINT ["./peercast-0yp"]
