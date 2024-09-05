FROM golang:1.22 AS build
ARG PROGRAM_VERSION=v1.0.0
WORKDIR /go/src
COPY . .
ENV CGO_ENABLED=0
RUN go mod download
RUN go build -ldflags="-X 'main.Version=${PROGRAM_VERSION}'" -a -o /go/bin/minijob /go/src/cmd/minijob/minijob.go

FROM alpine AS runtime
RUN apk --no-cache add curl
WORKDIR /app
COPY --from=build /go/bin/* ./
COPY config-templates/docker/config /app/config
EXPOSE 8080/tcp
HEALTHCHECK CMD curl -f http://localhost:8080/api/v1/utils/ping || exit 1
CMD ["/app/minijob", "-config", "/app/config/config.yaml"]
