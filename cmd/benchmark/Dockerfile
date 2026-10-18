# Load generator image, built from the repository root:
# docker build -t minijob-benchmark -f cmd/benchmark/Dockerfile .
FROM golang:1.27.2 AS build
WORKDIR /go/src
COPY go.mod go.sum ./
COPY cmd/benchmark ./cmd/benchmark
ENV CGO_ENABLED=0
RUN go build -o /benchmark ./cmd/benchmark

FROM alpine:3.22
COPY --from=build /benchmark /usr/local/bin/benchmark
ENTRYPOINT ["benchmark"]
