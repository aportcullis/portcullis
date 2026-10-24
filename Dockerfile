# syntax=docker/dockerfile:1

# Pin image digests for reproducible builds; Renovate updates tags and digests together.

FROM --platform=$BUILDPLATFORM node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS web
RUN corepack enable
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY web/package.json web/package.json
COPY tests/load/package.json tests/load/package.json
RUN pnpm install --frozen-lockfile
COPY web/ web/
RUN pnpm -C web build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine3.24@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/platform/assets/dist ./internal/platform/assets/dist
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /portcullis ./cmd/portcullis

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b
COPY --from=build /portcullis /portcullis
EXPOSE 8080
ENTRYPOINT ["/portcullis"]
