# The panel's container image.
#
# Three stages so that a change to the Go code does not reinstall npm packages,
# and so that the image that ships contains a single static binary and nothing
# else: no shell, no package manager, no interpreter to be exploited.

FROM node:22-alpine AS frontend
WORKDIR /src/web
# Dependencies first: this layer is reused until package-lock.json changes.
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
# CGO off: the SQLite driver is pure Go, so the result runs on a distroless
# image with no libc at all.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X skifity/internal/version.Version=${VERSION} \
      -X skifity/internal/version.Commit=${COMMIT} \
      -X skifity/internal/version.Date=${DATE}" \
    -o /out/skifity ./cmd/skifity

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=backend /out/skifity /usr/local/bin/skifity

# 65532 is distroless's "nonroot" user. The installer chowns the panel's
# directories to it, so the container never needs to run as root.
USER 65532:65532
WORKDIR /var/lib/skifity
EXPOSE 8080

# The health endpoint answers before the cluster connection is established, so
# a panel that cannot reach Kubernetes still reports why instead of restarting.
ENTRYPOINT ["/usr/local/bin/skifity"]
CMD ["server"]
