FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /delivery_drone ./cmd/delivery_drone

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=build /delivery_drone /delivery_drone
EXPOSE 8080
ENV HEALTH_PORT=8080
CMD ["/delivery_drone"]
