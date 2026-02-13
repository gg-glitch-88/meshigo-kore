# Stage 1: Builder
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 creates a static binary (no libc dependency)
RUN CGO_ENABLED=0 GOOS=linux go build -o my-app ./cmd/server

# Stage 2: Runner (Distroless is safer/smaller than Alpine)
FROM gcr.io/distroless/static-debian12
COPY --from=builder /app/my-app /
CMD ["/my-app"]