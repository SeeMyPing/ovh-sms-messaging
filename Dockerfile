# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/sqs-to-smpp-gateway ./cmd/sqs-to-smpp-gateway

# distroless/static ships the CA certificates needed for SMPP over TLS and the
# HTTP APIs.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sqs-to-smpp-gateway /sqs-to-smpp-gateway
EXPOSE 8080
ENTRYPOINT ["/sqs-to-smpp-gateway"]
