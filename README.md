# kafka-offset-lag-for-prometheus
Expose Kafka consumer offset lag to prometheus.

## Getting Started
This project is designed to run with Docker.
`docker-compose up -d`
Use the following ENV vars to change the default options:
* KAFKA_BROKERS=comma separated list of brokers
* PROMETHEUS_ADDR=address and port for Prometheus to bind to
* REFRESH_INTERVAL=how long in seconds between each refresh
* DEBUG=true or false

You may also build and run locally using cli arguments.  See the Dockerfile
for build instructions.
