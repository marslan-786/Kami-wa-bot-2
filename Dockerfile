# بلڈ سٹیج
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY . .
RUN go mod tidy
RUN go build -o bot .

# رن سٹیج
FROM alpine:latest
RUN apk add --no-cache sqlite-libs ca-certificates
WORKDIR /app
# ڈیٹا کے لیے فولڈر بنانا
RUN mkdir -p /app/data
COPY --from=builder /app/bot .
# اب یہاں VOLUME کی کمانڈ نہیں ہے، یہ ریلوے ڈیش بورڈ سے ہوگا
CMD ["./bot"]