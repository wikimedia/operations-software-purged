package main

import (
	"encoding/binary"
	"log"
	"net"
	"strings"
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

// Continuously read from the given multicast address, extract URLs to be
// purged and quickly offload the data to the provided buffered channel
// "churls".
func (pr MultiCastReader) readFromAddr(churls chan string, mcastAddr string) {
	addr, err := net.ResolveUDPAddr("udp4", mcastAddr)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		log.Fatal(err)
	}

	conn.SetReadBuffer(pr.maxDatagramSize)
	buffer := make([]byte, pr.maxDatagramSize)

	log.Printf("Reading from %s with maximum datagram size %d", mcastAddr, pr.maxDatagramSize)

	for {
		readBytes, src, err := conn.ReadFromUDP(buffer)
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
	for _, addr := range strings.Split(pr.mcastAddrs, ",") {
		go pr.readFromAddr(churls, addr)
	}
}
