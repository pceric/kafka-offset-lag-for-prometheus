package main

import (
	"log"
	"math"
	"strings"

	"github.com/Shopify/sarama"
)

//TopicSet TopicName -> PartitionNumber -> LogEndOffset
type topicSet map[string]map[int32]int64

type exporterClientV1 struct {
	exporterClient
	topicSet topicSet
}

//NewExporterClientV1 Constructor for original client
func NewExporterClientV1(brokers []string, kafkaConfig *sarama.Config) (ExporterClient, error) {

	kafkaConfig.Version = sarama.V0_9_0_0
	client, err := sarama.NewClient(brokers, kafkaConfig)

	result := exporterClientV1{topicSet: nil}
	result.client = client
	return &result, err
}

func (c *exporterClientV1) kafkaClient() sarama.Client {
	return c.client
}

func (c *exporterClientV1) updateClusterData() error {
	c.topicSet = make(topicSet)
	c.client.RefreshMetadata()
	// First grab our topics, all partiton IDs, and their offset head
	topics, err := c.client.Topics()
	if err != nil {
		log.Printf("Error fetching topics: %s", err.Error())
		return err
	}

	for _, topic := range topics {
		// Don't include internal topics
		if strings.HasPrefix(topic, "__") {
			continue
		}
		partitions, err := c.client.Partitions(topic)
		if err != nil {
			log.Printf("Error fetching partitions: %s", err.Error())
			continue
		}
		if *debug {
			log.Printf("Found topic '%s' with %d partitions", topic, len(partitions))
		}
		c.topicSet[topic] = make(map[int32]int64)
		for _, partition := range partitions {
			toff, err := c.client.GetOffset(topic, partition, sarama.OffsetNewest)
			if err != nil {
				log.Printf("Problem fetching offset for topic '%s', partition '%d'", topic, partition)
				continue
			}
			c.topicSet[topic][partition] = toff
		}
	}
	return nil
}

func (c *exporterClientV1) updateBrokerData(broker *sarama.Broker) {
	groups := c.groups(broker)

	for _, group := range groups {
		// This is not very efficient but the kafka API sucks
		for topic, data := range c.topicSet {
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
						c.reportLagMetric(group, topic, partition, math.Max(float64(data[partition]-block.Offset), 0))
						c.reportOffsetMetric(group, topic, partition, math.Max(float64(block.Offset), 0))
					}
				}
			}
		}
	}
}

//curl http://localhost:7979/metrics | grep ibul.fluentd.raw
//kafka-consumer-groups   --bootstrap-server $B_T_ACL   --command-config /home/avrudenko/strp/acl.test.config   --group "ibul.fluentd.raw"   --describe   --verbose
//"-enable-current-offset"
