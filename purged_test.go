package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"

	"testing"
)

func assertEquals(t *testing.T, a, b interface{}) {
	if a != b {
		t.Errorf("%v != %v", a, b)
	}
}

func assertNotErr(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Expecting err to be nil, got %v instead", err)
	}
}

func TestSendPurge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		assertEquals(t, req.URL.String(), "/wiki/Main_Page")
		assertEquals(t, req.Method, "PURGE")
		assertEquals(t, req.Host, "en.wikipedia.org")
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()
	parsedURL, _ := url.Parse(server.URL)

	tcpClient := NewTCPPurger(parsedURL.Host)
	status, err := tcpClient.Send("en.wikipedia.org", "/wiki/Main_Page")
	assertEquals(t, status, "200")
	assertEquals(t, err, nil)

	httpClient := NewHTTPPurger(parsedURL.Host)
	status, err = httpClient.Send("en.wikipedia.org", "/wiki/Main_Page")
	assertEquals(t, status, "200")
	assertEquals(t, err, nil)
}

func BenchmarkTCPSendPurge(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()

	parsedURL, _ := url.Parse(server.URL)

	tcpClient := NewTCPPurger(parsedURL.Host)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tcpClient.Send("en.wikipedia.org", "/wiki/Main_Page")
	}
}

func BenchmarkHTTPSendPurge(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Write([]byte(`OK`))
	}))
	defer server.Close()

	parsedURL, _ := url.Parse(server.URL)

	httpClient := NewHTTPPurger(parsedURL.Host)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		httpClient.Send("en.wikipedia.org", "/wiki/Main_Page")
	}
}
