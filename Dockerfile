# syntax=docker/dockerfile:1

# Cross-compiles on the build host's native platform (no QEMU needed for the
# Go build); the final stage has no RUN steps, so multi-arch builds stay fast.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath \
      -ldflags "-s -w -X github.com/sulaimantok/semar/internal/version.Version=${VERSION} -X github.com/sulaimantok/semar/internal/version.Commit=${COMMIT}" \
      -o /out/semar-api ./cmd/semar-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/semar-api /usr/local/bin/semar-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/semar-api"]
