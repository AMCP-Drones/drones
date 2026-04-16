# Multi-stage build: build in Go image, run in minimal image.
# No env in Dockerfile — all broker/config from environment in docker-compose.
# Uses vendored deps (-mod=vendor) so no network or git is required in the image.

FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY . .
RUN CGO_ENABLED=0 go build -mod=vendor -o /delivery-drone ./systems/deliverydron/delivery_drone/cmd/delivery_drone

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /delivery-drone /app/delivery-drone
EXPOSE 8080
CMD ["/app/delivery-drone"]
