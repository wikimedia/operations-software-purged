// Copyright (C) 2020 Emanuele Rocca <ema@wikimedia.org>
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
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/net/ipv4"
)

type PurgeReader interface {
	Read(c chan string)
}

type MultiCastReader struct {
	maxDatagramSize int
	// how big we try to set the kernel buffer via setsockopt()
	kbufSize   int
	mcastAddrs string
}

const (
	stateLabel = "state"
	goodValue  = "good"
	badValue   = "bad"
	minHTCPLen = 20
)

var (
	htcpPackets = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_htcp_packets_total",
		Help: "Total number of HTCP packets received",
	}, []string{
		stateLabel,
	})
	bytesRead = promauto.NewCounter(prometheus.CounterOpts{
		Name: "purged_udp_bytes_read_total",
		Help: "Total number of UDP bytes read",
	})
)

func extractURL(buffer []byte, n int) (string, error) {
	if buffer == nil {
		htcpPackets.With(prometheus.Labels{stateLabel: badValue}).Inc()
		return "", errors.New("Rejecting HTCP packet, buffer is nil")
	}

	bytesRead.Add(float64(n))

	if n < minHTCPLen {
		htcpPackets.With(prometheus.Labels{stateLabel: badValue}).Inc()
		return "", errors.New(fmt.Sprintf("Rejecting HTCP packet, size smaller than %v", minHTCPLen))
	}

	// CLR opcode
	if buffer[6] != 4 {
		htcpPackets.With(prometheus.Labels{stateLabel: badValue}).Inc()
		return "", errors.New("Rejecting HTCP packet, no CLR opcode")
	}

	// start offset for data section
	var offset uint16 = 14

	// Method field
	method_len := binary.BigEndian.Uint16(buffer[offset : offset+2])
	offset += 2

	// skip method
	offset += method_len

	// URL length field
	url_len := binary.BigEndian.Uint16(buffer[offset : offset+2])
	offset += 2

	if url_len == 0 {
		htcpPackets.With(prometheus.Labels{stateLabel: badValue}).Inc()
		return "", errors.New("Rejecting HTCP packet, URL len is zero")
	}

	// Good packet received
	htcpPackets.With(prometheus.Labels{stateLabel: goodValue}).Inc()

	return string(buffer[offset : offset+url_len]), nil
}

// Continuously read from the given multicast addresses, extract URLs to be
// purged and quickly offload the data to the provided buffered channel
// "churls".
func (pr MultiCastReader) readFromAddrs(churls chan string, mcastAddrs string) {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:4827")
	if err != nil {
		log.Fatal(err)
	}

	if err := conn.(*net.UDPConn).SetReadBuffer(pr.kbufSize); err != nil {
		log.Fatal(err)
	}

	p := ipv4.NewPacketConn(conn)

	for _, addr := range strings.Split(mcastAddrs, ",") {
		g := net.ParseIP(addr)

		if err := p.JoinGroup(nil, &net.UDPAddr{IP: g}); err != nil {
			log.Fatal(err)
		}
	}

	buffer := make([]byte, pr.maxDatagramSize)

	log.Printf("Reading from %s with maximum datagram size %d", mcastAddrs, pr.maxDatagramSize)

	for {
		readBytes, _, src, err := p.ReadFrom(buffer)
		if err != nil {
			log.Println("Error while reading from", src, "->", err)
			continue
		}

		url, err := extractURL(buffer, readBytes)
		if err != nil {
			log.Println(err)
			continue
		}

		churls <- url
	}
}

func (pr MultiCastReader) Read(churls chan string) {
	pr.readFromAddrs(churls, pr.mcastAddrs)
}
