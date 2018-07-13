package main

import (
	"flag"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	"github.com/kouhin/envflag"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	inactiveConsumers = flag.Bool("inactive-consumers", false, "Show all consumers including inactive ones.")
	kafkaBrokers      = flag.String("kafka-brokers", "localhost:9092", "Comma separated list of kafka brokers.")
	prometheusAddr    = flag.String("prometheus-addr", ":7979", "Prometheus listen interface and port.")
	refreshInt        = flag.Int("refresh-interval", 15, "Time between offset refreshes in seconds.")
	saslUser          = flag.String("sasl-user", "", "SASL username.")
	saslPass          = flag.String("sasl-pass", "", "SASL password.")
	debug             = flag.Bool("debug", false, "Enable debug output.")
)

type TopicSet map[string]map[int32]int64

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
		config := sarama.NewConfig()
		config.ClientID = "kafka-offset-lag-for-prometheus"
		config.Version = sarama.V0_9_0_0
		if *saslUser != "" {
			config.Net.SASL.User = *saslUser
			config.Net.SASL.Password = *saslPass
		}
		client, err := sarama.NewClient(strings.Split(*kafkaBrokers, ","), config)

		if err != nil {
			log.Fatal("Unable to connect to given brokers.")
		}

		defer client.Close()

		for {
			topicSet := make(TopicSet)
			time.Sleep(time.Duration(*refreshInt) * time.Second)
			timer := prometheus.NewTimer(LookupHist)
			client.RefreshMetadata()
			// First grab our topics, all partiton IDs, and their offset head
			topics, err := client.Topics()
			if err != nil {
				log.Printf("Error fetching topics: %s", err.Error())
				continue
			}

			for _, topic := range topics {
				// Don't include internal topics
				if strings.HasPrefix(topic, "__") {
					continue
				}
				partitions, err := client.Partitions(topic)
				if err != nil {
					log.Printf("Error fetching partitions: %s", err.Error())
					continue
				}
				if *debug {
					log.Printf("Found topic '%s' with %d partitions", topic, len(partitions))
				}
				topicSet[topic] = make(map[int32]int64)
				for _, partition := range partitions {
					toff, err := client.GetOffset(topic, partition, sarama.OffsetNewest)
					if err != nil {
						log.Printf("Problem fetching offset for topic '%s', partition '%d'", topic, partition)
						continue
					}
					topicSet[topic][partition] = toff
				}
			}

			// Prometheus SDK never TTLs out data points so tmp consumers last
			// forever. Ugly hack to clean up data points from time to time.
			if cycle >= 99 {
				OffsetLag.Reset()
				cycle = 0
			}
			cycle++

			var wg sync.WaitGroup

			// Now lookup our group data using the metadata
			for _, broker := range client.Brokers() {
				// Sarama plays russian roulette with the brokers
				broker.Open(client.Config())
				_, err := broker.Connected()
				if err != nil {
					log.Printf("Could not speak to broker %s. Your advertised.listeners may be incorrect.", broker.Addr())
					continue
				}

				wg.Add(1)

				go func(broker *sarama.Broker) {
					defer wg.Done()
					refreshBroker(broker, topicSet)
				}(broker)
			}

			wg.Wait()
			timer.ObserveDuration()
		}
	}()
	prometheusListen(*prometheusAddr)
}

func refreshBroker(broker *sarama.Broker, topicSet TopicSet) {
	groupsRequest := new(sarama.ListGroupsRequest)
	groupsResponse, err := broker.ListGroups(groupsRequest)

	if err != nil {
		log.Printf("Could not list groups: %s\n", err.Error())
		return
	}

	for group, t := range groupsResponse.Groups {
		// do we want to see all consumers, not just active ones?
		if !*inactiveConsumers && t != "consumer" {
			continue
		}
		// This is not very efficient but the kafka API sucks
		for topic, data := range topicSet {
			offsetsRequest := new(sarama.OffsetFetchRequest)
			offsetsRequest.Version = 1
			offsetsRequest.ConsumerGroup = group
			for partition := range data {
				offsetsRequest.AddPartition(topic, partition)
			}

			offsetsResponse, err := broker.FetchOffset(offsetsRequest)
			if err != nil {
				log.Printf("Could not get offset: %s\n", err.Error())
			}

			for _, blocks := range offsetsResponse.Blocks {
				for partition, block := range blocks {
					if *debug {
						log.Printf("Discovered group: %s, topic: %s, partition: %d, offset: %d\n", group, topic, partition, block.Offset)
					}
					// Offset will be -1 if the group isn't active on the topic
					if block.Offset >= 0 {
						// Because our offset operations aren't atomic we could end up with a negative lag
						lag := math.Max(float64(data[partition]-block.Offset), 0)

						OffsetLag.With(prometheus.Labels{
							"topic": topic, "group": group,
							"partition": strconv.FormatInt(int64(partition), 10),
						}).Set(lag)
					}
				}
			}
		}
	}
}
