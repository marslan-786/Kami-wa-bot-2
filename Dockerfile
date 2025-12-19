# بلڈ سٹیج
FROM golang:1.21-alpine AS builder
# یہاں 'git' ایڈ کر دیا گیا ہے
RUN apk add --no-cache gcc musl-dev sqlite-dev git
WORKDIR /app
COPY . .
RUN go mod tidy
RUN go build -o bot .

# رن سٹیج
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bot .
CMD ["./bot"]