# syntax=docker/dockerfile:1

FROM golang:1.27.1-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/app && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# Small runtime image with no shell, running as a non-root user.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/app /app
COPY --from=build /out/migrate /migrate
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/app"]
