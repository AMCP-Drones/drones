FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /stub_component ./cmd/stub_component

FROM alpine:3.19
RUN apk --no-cache add ca-certificates && \
    addgroup -g 1000 appgroup && \
    adduser -D -u 1000 -G appgroup appuser
COPY --from=build --chown=appuser:appgroup /stub_component /stub_component
ENV COMPONENT_TYPE=limiter
USER appuser
CMD ["/stub_component"]
