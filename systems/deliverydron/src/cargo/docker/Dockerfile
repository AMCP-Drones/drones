FROM golang:1.21-alpine AS build
WORKDIR /app
COPY . .
RUN go build -mod=vendor -o /stub_component ./cmd/stub_component

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
COPY --from=build /stub_component /stub_component
ENV COMPONENT_TYPE=cargo
CMD ["/stub_component"]
