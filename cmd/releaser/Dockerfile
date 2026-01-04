FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o releaser ./cmd/releaser

FROM alpine:latest

WORKDIR /app

# Install git as the releaser needs it to perform git operations
RUN apk add --no-cache git

COPY --from=builder /app/releaser .

ENTRYPOINT ["./releaser"]
