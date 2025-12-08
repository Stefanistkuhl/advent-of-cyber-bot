FROM golang:1.25 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -v -o /app/emoteCollector .

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/emoteCollector /app/emoteCollector
VOLUME ["/app/data"]
WORKDIR /app
CMD ["/app/emoteCollector"]
