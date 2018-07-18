# kafka-offset-lag-for-prometheus
Expose Kafka consumer offset lag to prometheus.

## Requirements
Kafka v0.9.0.0 or higher.  This exporter will only export offsets
stored in Kafka.

## Getting Started
This project is designed to run with Docker.

`docker-compose up -d`

Use the following ENV vars to change the default options:
* ACTIVE_ONLY=only show consumers with an active consumer protocol (default false)
* KAFKA_BROKERS=comma separated list of brokers (default localhost:9092)
* PROMETHEUS_ADDR=address and port for Prometheus to bind to (default :7979)
* REFRESH_INTERVAL=how long in seconds between each refresh (default 15)
* SASL_USER=SASL username if required (default "")
* SASL_PASS=SASL password if required (default "")
* DEBUG=true or false (default false)

You may also build and run locally using cli arguments.  See the Dockerfile
for build instructions.

## Prometheus
You can find the prometheus output at the standard `/metrics` path on the port you configured.

An example function might look like:
`sum by (group, topic) (max_over_time(kafka_consumer_group_lag[5m]))`
