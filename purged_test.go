package main

import (
	"net"
	"sync"
	"testing"
)

func assertNotErr(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Expecting err to be nil, got %v instead", err)
	}
}

func TestSendPurge(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	server, client := net.Pipe()

	buf := make([]byte, 4096)

	go func() {
		sendPurge(server, "en.wikipedia.org", "/wiki/Main_Page", "frontend")
		server.Close()
		wg.Done()
	}()

	expected := "PURGE /wiki/Main_Page HTTP/1.1\r\nHost: en.wikipedia.org\r\nUser-Agent: purged\r\n\r\n"

	nbytes, err := client.Read(buf)
	assertNotErr(t, err)
	response := string(buf[:nbytes])

	if response != expected {
		t.Errorf("PURGE request looks different than expected")
	}
	client.Close()
	wg.Wait()
}

func BenchmarkSendPurge(b *testing.B) {
	server, _ := net.Pipe()

	for i := 0; i < b.N; i++ {
		go func() {
			sendPurge(server, "en.wikipedia.org", "/wiki/Main_Page", "frontend")
		}()
	}
}
