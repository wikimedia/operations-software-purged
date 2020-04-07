package main

import (
	"encoding/binary"
	"log"
	"net"
)

type PurgeReader interface {
	Read(c chan string) error
}

type MultiCastReader struct {
	maxDatagramSize int
	bytesRead       int
	badPackets      int
	mcastAddr       string
}

// Continuously read from the given multicast address, convert bytes to string
// and quickly offload the data to a buffered channel. URLs to be purged are
// made available in the provided channel "churls".
func (pr MultiCastReader) Read(churls chan string) error {
	addr, err := net.ResolveUDPAddr("udp4", pr.mcastAddr)
	if err != nil {
		return err
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return err
	}

	conn.SetReadBuffer(pr.maxDatagramSize)
	buffer := make([]byte, pr.maxDatagramSize)

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
