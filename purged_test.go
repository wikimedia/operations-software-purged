package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
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

func assertListEquals(t *testing.T, a, b []string) {
	if len(a) != len(b) {
		t.Errorf("len(%v) != len(%v)", a, b)
	}

	sort.Strings(a)
	sort.Strings(b)

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			t.Errorf("a[%d] != b[%d] (%v != %v)", i, i, a[i], b[i])
		}
	}
}

func testWorkersWrapper(t *testing.T, re *regexp.Regexp, input []string, expected []string) {
	var feURLs []string
	var beURLs []string
	expectedLen := len(expected)

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
	testFrCh := make(chan url.URL, 10)

	for _, url := range input {
		testCh <- url
	}

	startWorkers(backendURL.Host, frontendURL.Host, testCh, testFrCh, re)

	// Wait for all URLs in the channel to be consumed
	for ; len(feURLs) < expectedLen || len(beURLs) < expectedLen; time.Sleep(100 * time.Millisecond) {
	}

	assertEquals(t, len(feURLs), len(beURLs))
	assertEquals(t, len(feURLs), expectedLen)

	assertListEquals(t, feURLs, expected)
	assertListEquals(t, beURLs, expected)
}

func TestWorkers(t *testing.T) {
	input := []string{
		"https://en.wikipedia.org/wiki/Main_Page",
		"https://it.wikipedia.org/wiki/Pagina_principale",
		"http://en.m.wikipedia.org/w/index.php?title=User_talk:127.0.0.1&action=history",
	}

	expected := []string{
		"/w/index.php?title=User_talk:127.0.0.1&action=history",
		"/wiki/Main_Page",
		"/wiki/Pagina_principale",
	}

	testWorkersWrapper(t, nil, input, expected)
}

func TestWorkersRegexp(t *testing.T) {
	re := regexp.MustCompile("[um][pa][lp][os]")
	input := []string{
		"https://en.wikipedia.org/wiki/Main_Page",
		"https://it.wikipedia.org/wiki/Pagina_principale",
		"https://upload.wikimedia.org/wikipedia/commons/thumb/7/78/Flag_of_Italy_%281861%E2%80%931946%29.svg/20px-Flag_of_Italy_%281861%E2%80%931946%29.svg.png",
		"http://en.m.wikipedia.org/w/index.php?title=User_talk:127.0.0.1&action=history",
	}

	expected := []string{
		"/wikipedia/commons/thumb/7/78/Flag_of_Italy_%281861%E2%80%931946%29.svg/20px-Flag_of_Italy_%281861%E2%80%931946%29.svg.png",
	}

	testWorkersWrapper(t, re, input, expected)
}
