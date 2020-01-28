package main

import (
	"log"
	"strconv"

	"github.com/Shopify/sarama"
	"github.com/prometheus/client_golang/prometheus"
)

//ExporterClient Main interface for data manipulation
type ExporterClient interface {
	kafkaClient() sarama.Client
	brokers() []*sarama.Broker
	groups(broker *sarama.Broker) []string
	updateClusterData() error
	updateBrokerData(broker *sarama.Broker)
	reportOffsetMetric(group string, topic string, partition int32, offset float64)
	reportLagMetric(group string, topic string, partition int32, lag float64)
}

type exporterClient struct {
	client sarama.Client
}

func (e *exporterClient) reportOffsetMetric(group string, topic string, partition int32, offset float64) {
	CurrentOffset.With(prometheus.Labels{
		"topic": topic, "group": group,
		"partition": strconv.FormatInt(int64(partition), 10),
	}).Set(offset)
}
func (e *exporterClient) reportLagMetric(group string, topic string, partition int32, lag float64) {
	if *enableCurrentOffset {
		OffsetLag.With(prometheus.Labels{
			"topic": topic, "group": group,
			"partition": strconv.FormatInt(int64(partition), 10),
		}).Set(lag)
	}
}

func (e *exporterClient) groups(broker *sarama.Broker) []string {
	var result []string

	groupsRequest := new(sarama.ListGroupsRequest)
	groupsResponse, err := broker.ListGroups(groupsRequest)

	if err != nil {
		log.Printf("Could not list groups: %s\n", err.Error())
		return result
	}

	for group, ptype := range groupsResponse.Groups {
		if *activeOnly && ptype != "consumer" {
			continue
		}
		result = append(result, group)
	}
	return result
}

func (e *exporterClient) brokers() []*sarama.Broker {
	var result []*sarama.Broker

	for _, broker := range e.client.Brokers() {
		// Sarama plays russian roulette with the brokers
		broker.Open(e.client.Config())
		_, err := broker.Connected()
		if err != nil {
			log.Printf("Could not speak to broker %s. Your advertised.listeners may be incorrect.", broker.Addr())
			continue
		}
		result = append(result, broker)
	}
	return result
}

func getConsumerConfig() *sarama.Config {
	config := sarama.NewConfig()
	config.ClientID = "kafka-offset-lag-for-prometheus"

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

	return config
}
