# Build stage. It runs on the build machine's architecture and cross-compiles
# for the target. GOTOOLCHAIN=local makes a go.mod bump fail loudly instead of
# silently downloading a newer toolchain.
FROM --platform=$BUILDPLATFORM golang:1.25.14-bookworm@sha256:3b4a11519ad929d1e1d261a12cff056f0c85b735253d7d861346b9c6f8b36437 AS build
ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
WORKDIR /src

# Dependencies come first so that source changes reuse this cached layer.
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd/ cmd/
COPY internal/ internal/
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/healthcheck ./cmd/healthcheck

# Runtime stage. Distroless has no shell or package manager, and it includes the
# CA certificates that TLS verification needs.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/healthcheck /usr/local/bin/healthcheck

# Numeric, so Kubernetes runAsNonRoot can verify it.
USER 65532:65532
EXPOSE 8080

# Exec form makes the API PID 1, so it receives SIGTERM directly.
ENTRYPOINT ["/usr/local/bin/api"]
