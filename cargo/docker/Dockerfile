FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
ARG DELIVERYDRON_ROOT=systems/deliverydron
WORKDIR /app/${DELIVERYDRON_ROOT}
RUN go build -mod=vendor -o /cargo ./cargo/cmd/cargo

FROM alpine:3.19
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser
COPY --from=build --chown=appuser:appgroup /cargo /cargo
USER appuser
CMD ["/cargo"]
