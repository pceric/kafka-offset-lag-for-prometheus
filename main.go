package main

import (
	"flag"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Shopify/sarama"
	"github.com/kouhin/envflag"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	kafkaBrokers   = flag.String("kafka-brokers", "localhost:9092", "Comma separated list of kafka brokers.")
	prometheusAddr = flag.String("prometheus-addr", ":7979", "Prometheus listen interface and port.")
	refreshInt     = flag.Int("refresh-interval", 15, "Time between offset refreshes in seconds.")
	debug          = flag.Bool("debug", false, "Enable debug output.")
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
		config := sarama.NewConfig()
		config.ClientID = "kafka-offset-lag-for-prometheus"
		config.Version = sarama.V0_9_0_0
		client, err := sarama.NewClient(strings.Split(*kafkaBrokers, ","), config)
		if err != nil {
			log.Fatal("Unable to connect to given brokers.")
		}
		defer client.Close()
		for {
			topicSet := make(map[string]map[int32]int64)
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
			// Now lookup our group data using the metadata
			for _, broker := range client.Brokers() {
				// Sarama plays russian roulette with the brokers
				broker.Open(client.Config())
				_, err := broker.Connected()
				if err != nil {
					log.Printf("Could not speak to broker %s. Your advertised.listeners may be incorrect.", broker.Addr())
					continue
				}
				grequest := new(sarama.ListGroupsRequest)
				gresponse, err := broker.ListGroups(grequest)
				if err != nil {
					log.Printf("Could not list groups: %s\n", err.Error())
					continue
				}
				for group, t := range gresponse.Groups {
					// we only want active consumers
					if t == "consumer" {
						// This is not very efficient but the kafka API sucks
						for topic, data := range topicSet {
							ofrequest := new(sarama.OffsetFetchRequest)
							ofrequest.Version = 1
							ofrequest.ConsumerGroup = group
							for partition := range data {
								ofrequest.AddPartition(topic, partition)
							}
							oresponse, err := broker.FetchOffset(ofrequest)
							if err != nil {
								log.Printf("Could not get offset: %s\n", err.Error())
							}
							for _, ofrb := range oresponse.Blocks {
								for gp, block := range ofrb {
									if *debug {
										log.Printf("Discovered group: %s, topic: %s, partition: %d, offset: %d\n", group, topic, gp, block.Offset)
									}
									// Offset will be -1 if the group isn't active on the topic
									if block.Offset >= 0 {
										// Because our offset operations aren't atomic we could end up with a negative lag
										lag := math.Max(float64(data[gp]-block.Offset), 0)
										OffsetLag.With(prometheus.Labels{"topic": topic, "group": group, "partition": strconv.FormatInt(int64(gp), 10)}).Set(lag)
									}
								}
							}
						}
					}
				}
			}
			timer.ObserveDuration()
		}
	}()
	prometheusListen(*prometheusAddr)
}
