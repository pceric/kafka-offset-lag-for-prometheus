FROM golang:1.13
LABEL maintainer "https://hub.docker.com/u/pceric/"
WORKDIR /go/src/kafka-offset-lag-for-prometheus
RUN useradd -ms /bin/sh kafkaoffsetlag
COPY . .
RUN go get -u "github.com/Shopify/sarama" \
              "github.com/kouhin/envflag" \
              "github.com/prometheus/client_golang/prometheus" \
              "github.com/prometheus/client_golang/prometheus/promhttp" \
              "github.com/xdg/scram"
RUN go install
USER kafkaoffsetlag
ENTRYPOINT ["/go/bin/kafka-offset-lag-for-prometheus"]
