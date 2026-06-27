# syntax=docker/dockerfile:1

# 1) Build the SolidJS frontend into the Go assets directory.
FROM node:24.18.0-alpine3.24 AS web
RUN corepack enable
WORKDIR /src
COPY web/ web/
RUN cd web && pnpm install --frozen-lockfile && pnpm build

# 2) Build the static Go binary with the frontend embedded.
FROM golang:1.26.4-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/platform/assets/dist ./internal/platform/assets/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /portcullis ./cmd/portcullis

# 3) Minimal runtime image: a single static binary as nonroot.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /portcullis /portcullis
EXPOSE 8080
ENTRYPOINT ["/portcullis"]
