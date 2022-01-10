package main

import (
	"flag"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"
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
	enableNewAPI        = flag.Bool("enable-new-api", false, "Enables new API, which allows to use optimized Kafka API calls")
	groupPattern        = flag.String("group-pattern", "", "Regular expression to filter consumer groups")
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

		var groupRegexp *regexp.Regexp
		if *groupPattern != "" {
			var err error
			groupRegexp, err = regexp.Compile(*groupPattern)
			if err != nil {
				log.Fatal("Failed to")
			}
		}

		config := sarama.NewConfig()
		config.ClientID = "kafka-offset-lag-for-prometheus"
		config.Version = sarama.V0_9_0_0
		if *enableNewAPI {
			config.Version = sarama.V0_10_2_0
		}
		if *saslUser != "" {
			config.Net.SASL.Enable = true
			config.Net.SASL.User = *saslUser
			config.Net.SASL.Password = *saslPass
		}
		if *algorithm == "sha512" {
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &XDGSCRAMClient{HashGeneratorFcn: SHA512} }
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA512)
		} else if *algorithm == "sha256" {
			config.Net.SASL.SCRAMClientGeneratorFunc = func() sarama.SCRAMClient { return &XDGSCRAMClient{HashGeneratorFcn: SHA256} }
			config.Net.SASL.Mechanism = sarama.SASLMechanism(sarama.SASLTypeSCRAMSHA256)
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
				CurrentOffset.Reset()
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
					if *enableNewAPI {
						refreshBrokerV2(broker, client, groupRegexp)
					} else {
						refreshBroker(broker, topicSet, groupRegexp)
					}
				}(broker)
			}

			wg.Wait()
			timer.ObserveDuration()
		}
	}()
	prometheusListen(*prometheusAddr)
}

func refreshBroker(broker *sarama.Broker, topicSet TopicSet, groupRegexp *regexp.Regexp) {
	groupsRequest := new(sarama.ListGroupsRequest)
	groupsResponse, err := broker.ListGroups(groupsRequest)

	if err != nil {
		log.Printf("Could not list groups: %s\n", err.Error())
		return
	}

	for group, ptype := range groupsResponse.Groups {
		// do we want to filter by active consumers?
		if *activeOnly && ptype != "consumer" {
			continue
		}
		if groupRegexp != nil && !groupRegexp.MatchString(group) {
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
						if *enableCurrentOffset {
							CurrentOffset.With(prometheus.Labels{
								"topic": topic, "group": group,
								"partition": strconv.FormatInt(int64(partition), 10),
							}).Set(math.Max(float64(block.Offset), 0))
						}
					}
				}
			}
		}
	}
}

func refreshBrokerV2(broker *sarama.Broker, client sarama.Client, groupRegexp *regexp.Regexp) {
	groupsRequest := new(sarama.ListGroupsRequest)
	groupsResponse, err := broker.ListGroups(groupsRequest)

	if err != nil {
		log.Printf("Could not list groups: %s\n", err.Error())
		return
	}

	for group, ptype := range groupsResponse.Groups {
		// do we want to filter by active consumers?
		if *activeOnly && ptype != "consumer" {
			continue
		}
		if groupRegexp != nil && !groupRegexp.MatchString(group) {
			continue
		}
		offsetsRequest := new(sarama.OffsetFetchRequest)
		offsetsRequest.Version = 2
		offsetsRequest.ConsumerGroup = group

		offsetsResponse, err := broker.FetchOffset(offsetsRequest)
		if err != nil {
			log.Printf("Could not get offset: %s\n", err.Error())
		}

		for topic, partitions := range offsetsResponse.Blocks {

			for partition, block := range partitions {
				if *debug {
					log.Printf("Discovered group: %s, topic: %s, partition: %d, offset: %d\n", group, topic, partition, block.Offset)
				}

				partitionLatestOffset, err := client.GetOffset(topic, partition, sarama.OffsetNewest)

				if err != nil {
					log.Printf("Failed to obtain Latest Offset for topic: %s, partition: %d", topic, partition)
				}

				lag := math.Max(float64(partitionLatestOffset-block.Offset), 0)
				OffsetLag.With(prometheus.Labels{
					"topic": topic, "group": group,
					"partition": strconv.FormatInt(int64(partition), 10),
				}).Set(lag)
				if *enableCurrentOffset {
					CurrentOffset.With(prometheus.Labels{
						"topic": topic, "group": group,
						"partition": strconv.FormatInt(int64(partition), 10),
					}).Set(math.Max(float64(block.Offset), 0))
				}
			}
		}
	}
}
