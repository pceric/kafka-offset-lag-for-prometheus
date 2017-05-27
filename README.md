# kafka-offset-lag-for-prometheus
Expose Kafka consumer offset lag to prometheus.

## Requirements
Kafka v0.9.0.0 or higher.  This exporter will only export offsets
stored in Kafka.

## Getting Started
This project is designed to run with Docker.
`docker-compose up -d`
Use the following ENV vars to change the default options:
* KAFKA_BROKERS=comma separated list of brokers (default localhost:9092)
* PROMETHEUS_ADDR=address and port for Prometheus to bind to (default :7979)
* REFRESH_INTERVAL=how long in seconds between each refresh (default 15)
* DEBUG=true or false (default false)

You may also build and run locally using cli arguments.  See the Dockerfile
for build instructions.
