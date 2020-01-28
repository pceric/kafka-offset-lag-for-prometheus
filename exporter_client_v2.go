package main

import (
	"log"
	"math"

	"github.com/Shopify/sarama"
)

type exporterClientV2 struct {
	exporterClient
}

//NewExporterClientV2 Constructor for updated client
func NewExporterClientV2(brokers []string, kafkaConfig *sarama.Config) (ExporterClient, error) {

	kafkaConfig.Version = sarama.V0_10_2_0
	client, err := sarama.NewClient(brokers, kafkaConfig)

	result := exporterClientV2{}
	result.client = client
	return &result, err
}

func (c *exporterClientV2) kafkaClient() sarama.Client {
	return c.client
}

func (c *exporterClientV2) updateClusterData() error {
	return c.client.RefreshMetadata()
}

func (c *exporterClientV2) updateBrokerData(broker *sarama.Broker) {
	groups := c.groups(broker)

	for _, group := range groups {
		offsetsRequest := new(sarama.OffsetFetchRequest)
		offsetsRequest.Version = 2
		offsetsRequest.ConsumerGroup = group

		offsetsResponse, err := broker.FetchOffset(offsetsRequest)
		if err != nil {
			log.Printf("Could not get offset: %s\n", err.Error())
		}

		for topic, blocks := range offsetsResponse.Blocks {

			for partition, block := range blocks {
				if *debug {
					log.Printf("Discovered group: %s, topic: %s, partition: %d, offset: %d\n", group, topic, partition, block.Offset)
				}

				partitionLatestOffset, err := c.client.GetOffset(topic, partition, sarama.OffsetNewest)

				if err != nil {
					log.Printf("Failed to obtain Latest Offset for topic: %s, partition: %d", topic, partition)
				}

				// Because our offset operations aren't atomic we could end up with a negative lag
				c.reportLagMetric(group, topic, partition, math.Max(float64(partitionLatestOffset-block.Offset), 0))
				c.reportOffsetMetric(group, topic, partition, math.Max(float64(block.Offset), 0))
			}
		}
	}
}
