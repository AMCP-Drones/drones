FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /delivery_drone ./systems/deliverydron/delivery_drone/cmd/delivery_drone

FROM alpine:3.19
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser
COPY --from=build --chown=appuser:appgroup /delivery_drone /delivery_drone
EXPOSE 8080
ENV HEALTH_PORT=8080
USER appuser
CMD ["/delivery_drone"]
