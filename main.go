package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/kouhin/envflag"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	activeOnly          = flag.Bool("active-only", false, "Show only consumers with an active consumer protocol.")
	kafkaBrokers        = flag.String("kafka-brokers", "localhost:9092", "Comma separated list of kafka brokers.")
	prometheusAddr      = flag.String("prometheus-addr", ":7979", "Prometheus listen interface and port.")
	refreshInt          = flag.Int("refresh-interval", 15, "Time between offset refreshes in seconds.")
	saslUser            = flag.String("sasl-user", "", "SASL username.")
	saslPass            = flag.String("sasl-pass", "", "SASL password.")
	debug               = flag.Bool("debug", false, "Enable debug output.")
	algorithm           = flag.String("algorithm", "", "The SASL algorithm sha256 or sha512 as mechanism")
	enableCurrentOffset = flag.Bool("enable-current-offset", false, "Enables metrics for current offset of a consumer group")
	enableNewProtocol   = flag.Bool("enable-new-protocol", false, "Enables new protocol, which allows to use optimized Kafka API calls. You`ll need Kafka of at least v0.10.2")
)

func init() {
	if err := envflag.Parse(); err != nil {
		panic(err)
	}
	if *debug {
		sarama.Logger = log.New(os.Stdout, "[Sarama] ", log.LstdFlags)
	}
}

func main() {
	go func() {
		var cycle uint8

		var exporterClient ExporterClient
		var err error

		if *enableNewProtocol {
			exporterClient, err = NewExporterClientV2(strings.Split(*kafkaBrokers, ","), getConsumerConfig())
		} else {
			exporterClient, err = NewExporterClientV1(strings.Split(*kafkaBrokers, ","), getConsumerConfig())
		}

		if err != nil {
			log.Fatal("Unable to connect to given brokers.")
		}

		defer exporterClient.kafkaClient().Close()

		for {

			time.Sleep(time.Duration(*refreshInt) * time.Second)
			timer := prometheus.NewTimer(LookupHist)

			// Prometheus SDK never TTLs out data points so tmp consumers last
			// forever. Ugly hack to clean up data points from time to time.
			if cycle >= 99 {
				OffsetLag.Reset()
				CurrentOffset.Reset()
				cycle = 0
			}
			cycle++

			var wg sync.WaitGroup

			// Now lookup our group data using the metadata
			for _, broker := range exporterClient.brokers() {
				// Sarama plays russian roulette with the brokers
				wg.Add(1)

				go func(broker *sarama.Broker) {
					defer wg.Done()
					exporterClient.updateBrokerData(broker)
				}(broker)
			}

			wg.Wait()
			timer.ObserveDuration()
		}
	}()
	prometheusListen(*prometheusAddr)
}
