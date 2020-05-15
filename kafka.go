// Copyright (C) 2020 Giuseppe Lavagetto <joe@wikimedia.org>
// Copyright (C) 2020 Wikimedia Foundation, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	// Debian still uses the github.com url
	// TODO: use vendoring and refer instead to
	// "gopkg.in/confluentinc/confluent-kafka-go.v1/kafka"
	"github.com/confluentinc/confluent-kafka-go/kafka"
)

// KafkaConsumerAPI represents the minimal api we expect from a consumer client. Useful for testing.
type KafkaConsumerAPI interface {
	Close() error
	SubscribeTopics([]string, kafka.RebalanceCb) error
	Events() chan kafka.Event
}

// KafkaReader allows to read purge events from Kafka.
type KafkaReader struct {
	// The kafka consumer
	Reader KafkaConsumerAPI

	// The topic to subscribe
	Topics []string

	// The maximum age of a purge to send.
	MaxAge time.Duration

	// Newest timestamp seen. This is a coarse measure of the lag in seconds.
	maxts map[string]time.Time

	// The channel for communicating execution is complete
	Done chan struct{}
}

// stats file to be used by prometheus-rdkafka-exporter
const kafkaStatsFile = "/tmp/purged-kafka-stats.json"

// Kafka Prometheus metrics
var purgeEvents = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "purged_events_received_total",
		Help: "Total number of events received from kafka",
	},
	[]string{"tag", "status", "topic"},
)

var purgeLag = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "purged_event_lag",
		Help: "Time passed since the most recent processed event",
	},
	[]string{"topic"},
)

// Load the kafka config from a file. Taken from atskafka.
func loadConfig(f string) *kafka.ConfigMap {
	var vals kafka.ConfigMap
	jsonConfig, err := ioutil.ReadFile(f)
	if err != nil {
		log.Fatal(err)
	}

	err = json.Unmarshal(jsonConfig, &vals)
	if err != nil {
		log.Fatal(err)
	}

	// Convert float64 values to int. When unmarshaling into an interface
	// value, json.Unmarshal uses float64 for numbers, while kafka.ConfigMap
	// expects integers.
	for k := range vals {
		if _, ok := vals[k].(float64); ok {
			vals[k] = int(vals[k].(float64))
		}
	}

	return &vals
}

// NewKafkaReader creates a new kafka consumer based on the configuration provided.
func NewKafkaReader(configFile string, topics []string, d chan struct{}, maxage int) (*KafkaReader, error) {
	config := loadConfig(configFile)
	consumer, err := kafka.NewConsumer(config)
	if err != nil {
		log.Println("Unable to create a kafka consumer from the configuration")
		return nil, err
	}
	m := time.Duration(maxage) * time.Second
	kr := KafkaReader{Reader: consumer, Topics: topics, Done: d, MaxAge: m, maxts: make(map[string]time.Time, len(topics))}

	return &kr, nil
}

// Sets the highest timestamp we met.
func (k *KafkaReader) setLag(t time.Time, topic string) {
	if _, ok := k.maxts[topic]; !ok {
		k.maxts[topic] = t
	} else if t.After(k.maxts[topic]) {
		k.maxts[topic] = t
	}
}

// GetLag returns the lag, as an integer number of nanoseconds.
// The lag is defined as the time elapsed since the timestamp of the most recent event processed.
func (k *KafkaReader) GetLag(topic string) float64 {
	// At startup we report 0 lag.
	maxts, ok := k.maxts[topic]
	if !ok || maxts.IsZero() {
		return 0
	}
	return float64(time.Now().Sub(maxts).Nanoseconds())
}

func (k *KafkaReader) manageEvent(event kafka.Event, c chan string) bool {
	consume := true
	switch e := event.(type) {
	case *kafka.Message:
		// Find the topic, if any
		topic := "-"
		if e.TopicPartition.Topic != nil {
			topic = *e.TopicPartition.Topic
		}
		// Get the url from the message value
		rc, err := NewResourceChangeFromJSON(&e.Value)
		tag := ""
		status := "discarded"
		if err != nil {
			// TODO - add a prometheus counter?
			log.Printf("Could not decode the message: %v\n", err)
		} else {
			if len(rc.Tags) > 0 {
				tag = rc.Tags[0]
			}
			sendMsg := true
			if k.MaxAge != 0 {
				ts := time.Since(rc.GetTS())
				// If the timestamp of this purge is the newest we've seen, register it here.
				k.setLag(rc.GetTS(), topic)
				if ts > k.MaxAge {
					sendMsg = false
					status = "expired"
				}
			}
			if sendMsg {
				status = "ok"
				c <- *rc.GetURL()
			}
		}
		purgeEvents.With(prometheus.Labels{"tag": tag, "status": status, "topic": topic}).Inc()
	case *kafka.Stats:
		// For now, save the stats to a file in /tmp. TODO: expose the data via prometheus?
		go func(ev *kafka.Stats) {
			stats, err := os.OpenFile(kafkaStatsFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				log.Printf("Unable to open kafka stats file: %v\n", err)
				return
			}
			defer stats.Close()
			if _, err := stats.Write([]byte(ev.String())); err != nil {
				log.Printf("Unable to save kafka stats file: %v\n", err)
			}
		}(e)
	case *kafka.Error:
		// TODO: when moving to a newer version of librdkafka, use e.IsFatal()
		log.Printf("Error (code %d) reading from kafka: %v", e.Code(), e.Error())
		consume = false
	}
	return consume
}

// Read reads messages from the kafka topics we're subscribing to, and returns the URL on the channel
func (k *KafkaReader) Read(c chan string) {
	err := k.Reader.SubscribeTopics(k.Topics, nil)
	if err != nil {
		log.Fatalf("Could not subscribe the topics %v: %v\n", k.Topics, err)
	}
	consume := true
	log.Printf("Start consuming topics %v from kafka", k.Topics)
	// Eventloop that gets messages from Events()
	for consume == true {
		select {
		case <-k.Done:
			consume = false
		case event := <-k.Reader.Events():
			consume = k.manageEvent(event, c)
		}
	}
	err = k.Reader.Close()
	// now close the channel so the main process can terminate
	defer close(k.Done)
	if err != nil {
		log.Fatalf("Error trying to close the subscription to kafka: %v\n", err)
	}
}
