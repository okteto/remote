FROM golang:1.13 as builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY * ./
RUN make

FROM alpine

COPY --from=builder /app/remote /usr/local/bin/remote
RUN chmod +x /usr/local/bin/remote

