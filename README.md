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
* ALGORITHM=The SASL algorithm sha256 or sha512 as mechanism (default "")
* ENABLE_CURRENT_OFFSET=Enable current offset consumer group metric (default false)
* ENABLE_NEW_API=Enables new API, which allows to use optimized Kafka API calls (default false)
* GROUP_PATTERN=Regular expression to filter consumer groups (default "")

For TLS support, you can use the following ENV vars:
* TLS=true For enabling TLS support (default false)
* CA=Path to CA file for verifying server certificate (default "")
* CERTIFICATE=Path to certificate file for client authentication (default "")
* KEY=Path to key file for client authentication (default "")
* SKIP_VERIFY=true If you want to skip server certificate verification (default false)

You may also build and run locally using cli arguments.  See the Dockerfile
for build instructions.

## Prometheus
You can find the prometheus output at the standard `/metrics` path on the port you configured.

An example function might look like:
`sum by (group, topic) (max_over_time(kafka_consumer_group_lag[5m]))`
