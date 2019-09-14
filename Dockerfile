FROM golang:1.13 as builder

WORKDIR /app

COPY go.mod .
COPY go.sum .
RUN go mod download

COPY * ./
RUN CGO=0 GOOS=linux go build -o remote -tags "osusergo netgo static_build" ./...

FROM alpine

COPY --from=builder /app/remote /usr/local/bin/okteto-remote
RUN chmod +x /usr/local/bin/okteto-remote

