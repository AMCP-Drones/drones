FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
ARG DELIVERYDRON_ROOT=systems/deliverydron
RUN go build -mod=vendor -o /emergency ./${DELIVERYDRON_ROOT}/emergency/cmd/emergency

FROM alpine:3.19
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser
COPY --from=build --chown=appuser:appgroup /emergency /emergency
USER appuser
CMD ["/emergency"]
