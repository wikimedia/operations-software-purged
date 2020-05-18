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
	"fmt"
	"testing"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
)

// A mock Kafka consumer
type MockConsumer struct {
	IsClosed  bool
	EventChan chan kafka.Event
	Topics    []string
}

func NewMockConsumer(ev chan kafka.Event) *MockConsumer {
	mc := MockConsumer{IsClosed: false, EventChan: ev}
	return &mc
}

func (m *MockConsumer) Close() error {
	if m.IsClosed {
		return fmt.Errorf("Trying to close an already closed consumer")
	}
	m.IsClosed = true
	return nil
}

func (m *MockConsumer) SubscribeTopics(t []string, cb kafka.RebalanceCb) error {
	m.Topics = t
	return nil
}
func (m *MockConsumer) Events() chan kafka.Event {
	return m.EventChan
}

// Setup the Kafka reader, send events on the mockconsumer.
func setupKafkaReaderTest(events [][]byte, inject bool) (*KafkaReader, *MockConsumer) {
	chansize := len(events) + 1
	eventchan := make(chan kafka.Event, chansize)
	mr := NewMockConsumer(eventchan)
	// Set MaxAge to 0 in most tests.
	kr := KafkaReader{Reader: mr, Topics: []string{"topic1", "topic2"}, MaxAge: 0, maxts: make(map[string]time.Time, 2)}
	// Now send the events
	go func(evts *[][]byte) {
		for _, eventData := range events {
			topicPartition := kafka.TopicPartition{Topic: &kr.Topics[0]}
			msg := kafka.Message{Value: eventData, TopicPartition: topicPartition}
			eventchan <- &msg
		}
		if inject {
			// Now also send a kafka error to ensure we close execution.
			ke := kafka.Error{}
			eventchan <- &ke
		}
	}(&events)
	return &kr, mr
}

// The lag should be zero upon initialization, and depend on maxts otherwise
func TestGetLag(t *testing.T) {
	events := [][]byte{}
	kr, _ := setupKafkaReaderTest(events, true)
	if kr.GetLag("topic1") != 0 {
		t.Error("Lag should be zero at startup")
	}
	// now set a TS on a new topic
	kr.setLag(time.Now().AddDate(0, -1, 0), "topic1")
	if kr.GetLag("topic1") == 0 {
		t.Error("Lag should be non-zero once we've set a max seen timestamp.")
	}
}

// Reading good messages should enqueue urls in the channel, while stale object
// should not.
func TestReadGoodMessage(t *testing.T) {
	events := [][]byte{
		[]byte(`{
			"$schema": "/resource_change/1.0.0",
			"meta": {
				"dt": "2020-04-30T11:37:53Z",
				"stream": "purge",
				"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
			},
			"tags": ["test"]
		}`),
	}

	kr, mr := setupKafkaReaderTest(events, true)
	c := make(chan string, 1)
	d := make(chan struct{})
	kr.Done = d
	kr.Read(c)
	if len(c) != 1 {
		t.Errorf("Found %d messages, 1 expected", len(c))
	}
	if mr.IsClosed == false {
		t.Errorf("The consumer was not closed")
	}
	url := <-c
	if url != "https://it.wikipedia.org/wiki/Francesco_Totti" {
		t.Errorf("Unexpected url transmitted: %v", url)
	}
	// Check that the lag has been set to non-zero values.
	if kr.GetLag("topic1") == 0 {
		t.Errorf("Expected the lag to be non-zero.")
	}
}

// A message produced before our maxage value gets discarded
func TestDiscardMessage(t *testing.T) {
	events := [][]byte{
		[]byte(`{
			"$schema": "/resource_change/1.0.0",
			"meta": {
				"dt": "2020-04-30T11:37:53Z",
				"stream": "purge",
				"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
			},
			"tags": ["test"]
		}`),
	}
	kr, _ := setupKafkaReaderTest(events, true)
	c := make(chan string, 1)
	d := make(chan struct{})
	kr.Done = d
	// This surely is later than april 2020 :)
	kr.MaxAge = time.Duration(1) * time.Second
	kr.Read(c)
	if len(c) != 0 {
		t.Errorf("Erroneously loaded message older than the current Maxage.")
	}
	// Even if the message was discarded, the lag should be registered.
	if kr.GetLag("topic1") == 0 {
		t.Errorf("Expected the lag to be non-zero.")
	}
}

// Test that a malformed message behaves as expected.
func TestBadJsonMessage(t *testing.T) {
	// The malformed message should just be discarded
	events := [][]byte{
		[]byte(`{]`),
		[]byte(`{
			"$schema": "/resource_change/1.0.0",
			"meta": {
				"dt": "2020-04-30T11:37:53Z",
				"stream": "purge",
				"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
			}
		}`),
	}
	kr, mr := setupKafkaReaderTest(events, true)
	c := make(chan string, 1)
	d := make(chan struct{})
	kr.Done = d
	kr.Read(c)
	if len(c) != 1 {
		t.Errorf("The consumer produced a message for a malformed content")
	}
	if mr.IsClosed == false {
		t.Errorf("The consumer was not closed")
	}
}

// A message without a URI gets ignored.
func TestBadMessage(t *testing.T) {
	events := [][]byte{
		[]byte(`{
			"$schema": "/resource_change/1.0.0",
			"meta": {
				"dt": "2020-04-30T11:37:53Z",
				"stream": "purge"
			}
		}`),
	}
	// Do not inject errors, to check the test ends by just
	kr, _ := setupKafkaReaderTest(events, false)
	c := make(chan string, 1)
	d := make(chan struct{})
	kr.Done = d
	// Send the "done" message after 1 second
	go func() {
		time.Sleep(1 * time.Second)
		d <- struct{}{}
	}()
	kr.Read(c)
	if len(c) != 0 {
		t.Errorf("A message was produced for a message without a URL")
	}
}

func BenchmarkManageFullEvent(b *testing.B) {
	eventchan := make(chan kafka.Event, 1)
	// Set a ludicrously high maxage.
	// Anything generated after the birthday of wikipedia would work.
	maxage := time.Duration(169224) * time.Hour
	mr := NewMockConsumer(eventchan)
	kr := KafkaReader{Reader: mr, Topics: []string{"topic1", "topic2"}, MaxAge: maxage, maxts: make(map[string]time.Time)}
	evdata := []byte(`{
		"$schema":"/resource_change/1.0.0",
		"meta":{
			"stream":"change-prop.transcludes.resource-change",
			"uri":"https://en.wikipedia.org/wiki/Some_Page",
			"request_id":"XrADADADSA",
			"domain":"en.wikipedia.org",
			"dt":"2020-05-03T13:10:05.780Z",
			"id":"11111111111"
		},
		"triggered_by":"req:AAAAAAAAAAAAAAAAAAAAAA,mediawiki.revision-create:https://en.wikipedia.org/wiki/Another_Page",
		"tags":["transcludes","templates"],
		"root_event":{"signature":"https://en.wikipedia.org/wiki/Another_Page","dt":"2020-02-01T10:10:10Z"}
	}
	`)
	e := kafka.Message{Value: evdata}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := make(chan string, 1)
		kr.manageEvent(&e, c)
		close(c)
	}
}
