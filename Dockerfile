# syntax=docker/dockerfile:1

# Pin image digests for reproducible builds; Renovate updates tags and digests together.

FROM node:24.18.0-alpine3.24@sha256:a0b9bf06e4e6193cf7a0f58816cc935ff8c2a908f81e6f1a95432d679c54fbfd AS web
RUN corepack enable
WORKDIR /src
COPY web/ web/
RUN cd web && pnpm install --frozen-lockfile && pnpm build

FROM golang:1.27.1-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/platform/assets/dist ./internal/platform/assets/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /portcullis ./cmd/portcullis

FROM gcr.io/distroless/static-debian12:nonroot@sha256:aef9602f8710ec12bde19d593fed1f76c708531bb7aba205110f1029786ead7b
COPY --from=build /portcullis /portcullis
EXPOSE 8080
ENTRYPOINT ["/portcullis"]
