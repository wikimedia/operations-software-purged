package main

import (
	"encoding/binary"
	"log"
	"net"
	"strings"

	"golang.org/x/net/ipv4"
)

type PurgeReader interface {
	Read(c chan string)
}

type MultiCastReader struct {
	maxDatagramSize int
	bytesRead       int
	badPackets      int
	mcastAddrs      string
}

// Continuously read from the given multicast addresses, extract URLs to be
// purged and quickly offload the data to the provided buffered channel
// "churls".
func (pr MultiCastReader) readFromAddrs(churls chan string, mcastAddrs string) {
	conn, err := net.ListenPacket("udp4", "0.0.0.0:4827")
	if err != nil {
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

		pr.bytesRead += readBytes

		// CLR opcode
		if buffer[6] != 4 {
			pr.badPackets++
			log.Println("Rejecting HTCP packet, no CLR opcode")
			continue
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
			pr.badPackets++
			log.Println("Rejecting HTCP packet, URL len is zero")
			continue
		}

		churls <- string(buffer[offset : offset+url_len])
	}
}

func (pr MultiCastReader) Read(churls chan string) {
	pr.readFromAddrs(churls, pr.mcastAddrs)
}
