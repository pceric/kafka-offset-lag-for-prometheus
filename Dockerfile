FROM golang:1.15
LABEL maintainer "https://hub.docker.com/u/pceric/"
WORKDIR /go/src/kafka-offset-lag-for-prometheus
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o app

FROM scratch
COPY --from=0 /go/src/kafka-offset-lag-for-prometheus/app /kafka-offset-lag-for-prometheus
USER 1000
ENTRYPOINT ["/kafka-offset-lag-for-prometheus"]
