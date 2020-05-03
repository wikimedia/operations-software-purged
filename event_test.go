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
	"testing"
	"time"
)

// Ensure we can interpret a correctly-formatted resource_change event
func TestNewResourceChangeFromJSON(t *testing.T) {
	eventData := []byte(`{
		"$schema": "/resource_change/1.0.0",
		"meta": {
			"id": "aaaaaaaa-bbbb-bbbb-bbbb-123456789012",
			"dt": "2020-04-30T11:37:53.351Z",
			"stream": "purge",
			"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
		},
		"root_event": {
			"dt": "2020-04-24T09:00:00Z",
			"signature": ""
		}
	}`)
	event, err := NewResourceChangeFromJSON(&eventData)
	if err != nil {
		t.Errorf("Error loading a good event from json: %v", err)
	}
	url := *event.GetURL()
	if url != "https://it.wikipedia.org/wiki/Francesco_Totti" {
		t.Errorf("The url found in the event is %s (https://it.wikipedia.org/wiki/Francesco_Totti) expected", url)
	}
	rootTime := event.GetTS()
	ts := rootTime.Format("2006-01-02")
	if ts != "2020-04-24" {
		t.Errorf("The timestamp found does not correspond: expected 2020-04-24, got %s", ts)
	}
}

// Ensure we can interpret a stripped-down resource_change event
func TestNewResourceChangeFromJSONSparse(t *testing.T) {
	eventData := []byte(`{
		"$schema": "/resource_change/1.0.0",
		"meta": {
			"dt": "2020-04-30T11:37:53Z",
			"stream": "purge",
			"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
		}
	}`)
	event, err := NewResourceChangeFromJSON(&eventData)
	if err != nil {
		t.Errorf("Error loading event: %v", err)
	}
	url := *event.GetURL()
	if url != "https://it.wikipedia.org/wiki/Francesco_Totti" {
		t.Errorf("The url found in the event is %s (https://it.wikipedia.org/wiki/Francesco_Totti) expected", url)
	}
	rootTime := event.GetTS()
	ts := rootTime.Format("2006-01-02")
	if ts != "2020-04-30" {
		t.Errorf("The timestamp found does not correspond: expected 2020-04-30, got %s", ts)
	}
}

func TestNewResourceChangeFromJSONNoUrl(t *testing.T) {
	eventData := []byte(`{
		"$schema": "/resource_change/1.0.0",
		"meta": {
			"dt": "2020-04-30T11:37:53Z",
			"stream": "purge"
		}
	}`)
	_, err := NewResourceChangeFromJSON(&eventData)
	if err == nil {
		t.Errorf("No error returned when loading an event without a URL")
	}
}

// We want to be resilient to bad formatting of datetimes. When we encounter a badly-formatted date
// we should log it, and use the current timestamp as a fallback instead.
func TestNewResourceChangeFromJSONBadDt(t *testing.T) {
	eventData := []byte(`{
		"$schema": "/resource_change/1.0.0",
		"meta": {
			"dt": "2020-04-30 11:37:53",
			"stream": "purge",
			"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
		}
	}`)
	ev, err := NewResourceChangeFromJSON(&eventData)
	if err != nil {
		t.Errorf("Parsing a bad date causes an error: %v", err)
	}
	if time.Since(ev.GetTS()) > time.Duration(5)*time.Hour {
		t.Errorf("Parsing a bad date didn't yield the current time.")
	}

}

// Simple benchmark of json decoding
func BenchmarkDecodeResourceChange(b *testing.B) {
	eventData := []byte(`{
		"$schema": "/resource_change/1.0.0",
		"meta": {
			"dt": "2020-04-30T11:37:53Z",
			"stream": "purge",
			"uri": "https://it.wikipedia.org/wiki/Francesco_Totti"
		},
		"root_event": {"signature": "abcd", "dt": "2020-04-29T11:37:53+02:00"}
	}`)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rc, _ := NewResourceChangeFromJSON(&eventData)
		rc.GetTS()
		rc.GetURL()
	}
}
