# Builds the control plane (api, worker, and the CLI tools) as one multi-command image.
# Community-edition image: no Manager or hosted per-tenant SES tooling (see backend/Dockerfile
# in the private repository for those).
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -o /out/worker ./cmd/worker \
 && CGO_ENABLED=0 go build -o /out/bootstrap ./cmd/bootstrap \
 && CGO_ENABLED=0 go build -o /out/bootstrap-owner ./cmd/bootstrap-owner \
 && CGO_ENABLED=0 go build -o /out/blobgw ./cmd/blobgw \
 && CGO_ENABLED=0 go build -o /out/stalwart-blobstore ./cmd/stalwart-blobstore \
 && CGO_ENABLED=0 go build -o /out/blobmigrate ./cmd/blobmigrate \
 && CGO_ENABLED=0 go build -o /out/mailprobe ./cmd/mailprobe \
 && CGO_ENABLED=0 go build -o /out/swprobe ./cmd/swprobe

FROM alpine:3.20
# ffmpeg/ffprobe: the worker's offline recording assembly needs both.
RUN apk add --no-cache ca-certificates curl ffmpeg
COPY --from=build /out/api /out/worker /out/bootstrap /out/bootstrap-owner /out/blobgw /out/stalwart-blobstore /out/blobmigrate /out/mailprobe /out/swprobe /usr/local/bin/
# Runs as an unprivileged user: a compromise here does not become root on the host.
RUN adduser -D -H -u 10001 beskos
USER beskos
# 'api' by default; override with `command:` for worker/bootstrap/bootstrap-owner.
CMD ["/usr/local/bin/api"]
