package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"time"

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

func TestWorkers(t *testing.T) {
	var feURLs []string
	var beURLs []string

	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		beURLs = append(beURLs, req.URL.String())
		rw.Write([]byte(`OK`))
	}))
	defer backend.Close()
	backendURL, _ := url.Parse(backend.URL)

	frontend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		feURLs = append(feURLs, req.URL.String())
		rw.Write([]byte(`OK`))
	}))
	defer frontend.Close()
	frontendURL, _ := url.Parse(frontend.URL)

	testCh := make(chan string, 10)

	testCh <- "https://en.wikipedia.org/wiki/Main_Page"
	testCh <- "https://it.wikipedia.org/wiki/Pagina_principale"

	startWorkers(backendURL.Host, frontendURL.Host, testCh)

	// Wait for all URLs in the channel to be consumed
	for ; len(feURLs) < 2 && len(beURLs) < 2; time.Sleep(100 * time.Millisecond) {
	}

	assertEquals(t, len(feURLs), len(beURLs))
	assertEquals(t, len(feURLs), 2)

	sort.Strings(feURLs)
	sort.Strings(beURLs)

	assertEquals(t, sort.SearchStrings(feURLs, "/wiki/Main_Page"), 0)
	assertEquals(t, sort.SearchStrings(feURLs, "/wiki/Pagina_principale"), 1)
	assertEquals(t, sort.SearchStrings(beURLs, "/wiki/Main_Page"), 0)
	assertEquals(t, sort.SearchStrings(beURLs, "/wiki/Pagina_principale"), 1)
}
