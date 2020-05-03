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
	"fmt"
	"log"
	"time"
)

// The json schema for resource_change messages can be found at
// https://schema.wikimedia.org/repositories/primary/jsonschema/resource_change/1.0.0.json
// We only ingest data that we currently use or that we expect to use in the future.

// RcTime is a simple container for time objects coming from the wire.
// Using such a struct allows us to implement json unmarshalling.
type RcTime struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *RcTime) UnmarshalJSON(b []byte) (err error) {
	// 'date-time' in jsonschema is the RFC3339 "internet-time"
	// https://tools.ietf.org/html/rfc3339#section-5.6
	data := string(b)
	data = data[1 : len(data)-1]
	parsed, err := time.Parse(time.RFC3339, data)
	if err == nil {
		t.Time = parsed
	} else {
		// we don't want to fail if we get an invalid date format. We'd rather log it.
		log.Printf("Invalid timestamp found: %s", data)
		t.Time = time.Now()
	}
	// So, we never fail unmarshalling dates, because it's auxillary information.
	return nil
}

// RcEvent contains the data for the resource_change event
type RcEvent struct {
	// UTC event datetime, in ISO-8601 format
	Dt RcTime `json:"dt"`

	// Unique URI identifying the event or entity
	URI *string `json:"uri"`
}

// RcRootEvent is the Unique identifier of the root event that triggered this event creation
type RcRootEvent struct {
	// the timestamp of the root event, in ISO-8601 format
	Dt RcTime `json:"dt"`
}

// ResourceChange represents a change in a resource tied to the specified URI
type ResourceChange struct {
	// Meta corresponds to the JSON schema field "meta".
	Event RcEvent `json:"meta"`

	// Unique identifier of the root event that triggered this event creation
	RootEvent *RcRootEvent `json:"root_event,omitempty"`

	// the list of tags associated with the change event for the resource
	Tags []string `json:"tags,omitempty"`
}

// NewResourceChangeFromJSON returns a resource change object from Json
func NewResourceChangeFromJSON(data *[]byte) (*ResourceChange, error) {
	var rc ResourceChange
	if err := json.Unmarshal(*data, &rc); err != nil {
		return nil, err
	}
	// We don't want objects without an url.
	if rc.Event.URI == nil {
		return nil, fmt.Errorf("The message didn't contain a valid URL")
	}
	return &rc, nil
}

// GetURL returns the url of the event
func (rc *ResourceChange) GetURL() *string {
	return rc.Event.URI
}

// GetTS gets the timestamp at which the event was originated.
// This is either the time of the event itself, or, if defined, the time of the root event.
func (rc *ResourceChange) GetTS() time.Time {
	// There is no root TS, so we return just the time of the event.
	if rc.RootEvent == nil {
		return rc.Event.Dt.Time
	}
	return rc.RootEvent.Dt.Time
}
