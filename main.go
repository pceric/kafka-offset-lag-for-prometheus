package main

import (
	"flag"
	"log"
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
	prometheusAddr = flag.String("prometheus-addr", "localhost:7979", "Prometheus listen interface and port.")
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
	type groupData struct {
		Topic           string
		PartitionOffset map[int32]int64
	}

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
			groupSet := make(map[string]*groupData)
			timer := prometheus.NewTimer(LookupHist)
			client.RefreshMetadata()
			// First lookup our group data using the metadata
			for _, broker := range client.Brokers() {
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
						ofrequest := new(sarama.OffsetFetchRequest)
						ofrequest.Version = 1
						ofrequest.ConsumerGroup = group
						oresponse, err := broker.FetchOffset(ofrequest)
						if err != nil {
							log.Printf("Could not get offset: %s\n", err.Error())
						}
						for topic, p := range oresponse.Blocks {
							gd := new(groupData)
							gd.Topic = topic
							gd.PartitionOffset = make(map[int32]int64)
							for partition, block := range p {
								if *debug {
									log.Printf("Adding group: %s, topic: %s, partition: %d, offset: %d\n", group, topic, partition, block.Offset)
								}
								gd.PartitionOffset[partition] = block.Offset
							}
							groupSet[group] = gd
						}
					}
				}
			}
			// Now grab the topic data and calculate lag
			for group, data := range groupSet {
				for partition, offset := range data.PartitionOffset {
					toff, err := client.GetOffset(data.Topic, partition, sarama.OffsetNewest)
					if err != nil {
						log.Printf("Problem fetching offset for topic '%s', partition '%d'", data.Topic, partition)
						continue
					}
					lag := float64(toff - offset)
					OffsetLag.With(prometheus.Labels{"topic": data.Topic, "group": group, "partition": strconv.FormatInt(int64(partition), 10)}).Set(lag)
				}
			}
			timer.ObserveDuration()
			time.Sleep(time.Duration(*refreshInt) * time.Second)
		}
	}()
	prometheusListen(*prometheusAddr)
}
