# --platform=$BUILDPLATFORM pins the build stage to the BUILDER's
# architecture so the Go toolchain runs natively and cross-compiles, rather
# than the whole stage running under QEMU. This workstation is arm64 and the
# cluster is amd64, so without it a `--platform linux/amd64` build silently
# produces an arm64 binary inside an amd64 image, which fails only at pod
# start with "exec format error".
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/adguard-exporter ./cmd/adguard-exporter

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/adguard-exporter /adguard-exporter
EXPOSE 9618
USER nonroot:nonroot
ENTRYPOINT ["/adguard-exporter"]
