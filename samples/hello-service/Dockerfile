# syntax=docker/dockerfile:1
# Multi-stage build: pinned toolchain image for build, distroless non-root for
# runtime (minimal attack surface for Trivy scanning, SR-4.4).
FROM golang:1.25-alpine AS build

WORKDIR /src
# Zero external dependencies — no go.sum download step.
COPY go.mod ./
COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION}" \
      -o /out/hello-service .

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="evdr-hello-service" \
      org.opencontainers.image.description="Phase 0 CI/CD pipeline validation service" \
      org.opencontainers.image.source="https://github.com/ivenkwan/EVDR"

COPY --from=build /out/hello-service /hello-service

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/hello-service"]
