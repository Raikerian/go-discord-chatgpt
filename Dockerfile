FROM golang:1.24.3-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main ./cmd/main.go

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -s /bin/sh appuser
WORKDIR /app

COPY --from=builder /app/main .
RUN chmod +x main

USER appuser

CMD ["./main"]
