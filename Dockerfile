FROM golang:1.13 as builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY Makefile /app
COPY pkg /app/pkg
COPY cmd /app/cmd
COPY .git /app/.git

RUN git rev-parse --short HEAD
RUN make

FROM alpine

COPY --from=builder /app/remote /usr/local/bin/remote
RUN chmod +x /usr/local/bin/remote

