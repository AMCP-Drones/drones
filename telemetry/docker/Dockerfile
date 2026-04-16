FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /telemetry ./systems/deliverydron/telemetry/cmd/telemetry

FROM alpine:3.19
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser
COPY --from=build --chown=appuser:appgroup /telemetry /telemetry
USER appuser
CMD ["/telemetry"]
