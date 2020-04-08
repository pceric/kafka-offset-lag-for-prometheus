FROM golang:1.13
LABEL maintainer "https://hub.docker.com/u/pceric/"
WORKDIR /go/src/kafka-offset-lag-for-prometheus
RUN go get -v -u "github.com/Shopify/sarama" \
              "github.com/kouhin/envflag" \
              "github.com/prometheus/client_golang/prometheus" \
              "github.com/prometheus/client_golang/prometheus/promhttp" \
              "github.com/xdg/scram"
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o app

FROM scratch
COPY --from=0 /go/src/kafka-offset-lag-for-prometheus/app /kafka-offset-lag-for-prometheus
USER 1000
ENTRYPOINT ["/kafka-offset-lag-for-prometheus"]
