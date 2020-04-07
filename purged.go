package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	frontendURL   = flag.String("frontend_url", "http://127.0.0.1:80", "Cache frontend URL")
	backendURL    = flag.String("backend_url", "http://127.0.0.1:3128", "Cache backend URL")
	mcastAddrs    = flag.String("mcast_addrs", "239.128.0.112:4827,239.128.0.115:4827", "Comma separated list of multicast addresses")
	metricsAddr   = flag.String("prometheus_addr", ":2112", "TCP network address for prometheus metrics")
	concurrency   = flag.Int("concurrency", runtime.NumCPU(), "Number of purger goroutines")
	purgeRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_http_requests_total",
		Help: "Total number of HTTP PURGE sent by status code",
	}, []string{
		"status",
		"url",
	})
	backlog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "purged_backlog",
		Help: "Number of messages still to process",
	})
)

func sendPurge(client *http.Client, baseUrl, path, host string) error {
	rawurl := baseUrl + path
	req, err := http.NewRequest("PURGE", rawurl, nil)
	if err != nil {
		log.Printf("Failed creating request for Host: %s URL=%s. %s\n", host, rawurl, err)
		return err
	}

	req.Header.Add("Host", host)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed sending request to Host: %s -> %s: %s\n", host, rawurl, err)
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(ioutil.Discard, resp.Body)
	if err != nil {
		log.Println(err)
		return err
	}

	purgeRequests.With(prometheus.Labels{"status": strconv.Itoa(resp.StatusCode), "url": baseUrl}).Inc()
	return err
}

func worker(ch chan string) {
	// Override DefaultTransport and keep it simple: establish one TCP
	// connection to the backend and one to the frontend for each goroutine.
	var netTransport = &http.Transport{}
	client := &http.Client{Transport: netTransport}

	for rawURL := range ch {
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			log.Println("Error parsing", rawURL, err)
			continue
		}

		sendPurge(client, *backendURL, parsedURL.Path, parsedURL.Host)
		sendPurge(client, *frontendURL, parsedURL.Path, parsedURL.Host)
	}
}

func main() {
	flag.Parse()

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, nil)
	}()

	// Setup reader
	pr := MultiCastReader{maxDatagramSize: 4096, mcastAddrs: *mcastAddrs}
	ch := make(chan string, 1000000)
	// Begin producing URLs to ch
	go pr.Read(ch)

	for i := 0; i < *concurrency; i++ {
		go worker(ch)
	}

	log.Printf("Process purged started with %d workers. Metrics at %s/metrics\n", *concurrency, *metricsAddr)

	for {
		// Update purged_backlog metric
		time.Sleep(1000 * time.Millisecond)
		backlog.Set(float64(len(ch)))
	}
}
