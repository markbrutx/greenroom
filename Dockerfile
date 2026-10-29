FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/moored ./cmd/moored \
    && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
LABEL org.opencontainers.image.source="https://github.com/markbrutx/moored" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.title="Moored" \
      org.opencontainers.image.description="Meet-style retro rooms in a single binary"
COPY --from=builder --chown=65532:65532 /out/data /data
COPY --from=builder /out/moored /usr/local/bin/moored
ENV MOORED_ADDR=:8080 \
    MOORED_DATABASE_URL=sqlite:/data/moored.db
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/moored", "health"]
ENTRYPOINT ["/usr/local/bin/moored"]
CMD ["serve"]
