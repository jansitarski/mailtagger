# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS build

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -trimpath \
    -o /out/mailtagger ./cmd/mailtagger

# Final image
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/mailtagger /usr/local/bin/mailtagger

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/mailtagger"]
CMD ["serve", "--config=/etc/mailtagger/config.yaml"]
