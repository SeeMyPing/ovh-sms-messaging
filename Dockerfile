# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/ovh-sms-messaging ./cmd/ovh-sms-messaging

# distroless/static ships the CA certificates needed to call OVH over HTTPS.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ovh-sms-messaging /ovh-sms-messaging
EXPOSE 8080
ENTRYPOINT ["/ovh-sms-messaging"]
