FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /mission_handler ./components/mission_handler/cmd/mission_handler

FROM alpine:3.19
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser
COPY --from=build --chown=appuser:appgroup /mission_handler /mission_handler
USER appuser
CMD ["/mission_handler"]
