ARG GO_VERSION=1
FROM golang:${GO_VERSION}-bookworm as builder

WORKDIR /usr/src/app
COPY go.mod go.sum ./
RUN go mod download 
RUN go mod verify
COPY . .
RUN go build -v -o /coord ./cmd/coord/main.go

FROM debian:bookworm

COPY --from=builder /coord /usr/local/bin/
CMD ["/usr/local/bin/coord"]
