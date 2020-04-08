package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	frontendAddr  = flag.String("frontend_addr", "127.0.0.1:80", "Cache frontend address")
	backendAddr   = flag.String("backend_addr", "127.0.0.1:3128", "Cache backend address")
	mcastAddrs    = flag.String("mcast_addrs", "239.128.0.112:4827,239.128.0.115:4827", "Comma separated list of multicast addresses")
	metricsAddr   = flag.String("prometheus_addr", ":2112", "TCP network address for prometheus metrics")
	concurrency   = flag.Int("concurrency", 4, "Number of purger goroutines")
	purgeRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_http_requests_total",
		Help: "Total number of HTTP PURGE sent by status code",
	}, []string{
		"status",
		"layer",
	})
	backlog = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "purged_backlog",
		Help: "Number of messages still to process",
	})
	bytesWritten = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_bytes_written_total",
		Help: "Total number of bytes sent by layer",
	}, []string{
		"layer",
	})
	bytesRead = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_bytes_read_total",
		Help: "Total number of bytes received by layer",
	}, []string{
		"layer",
	})
)

const purgeReq = "PURGE %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: purged\r\n\r\n"

func sendPurge(conn net.Conn, host, uri, layer string) error {
	nbytes, err := fmt.Fprintf(conn, purgeReq, uri, host)
	if err != nil {
		return err
	}

	// Update purged_bytes_written_total
	bytesWritten.With(prometheus.Labels{"layer": layer}).Add(float64(nbytes))

	buffer := make([]byte, 4096)
	nbytes, err = conn.Read(buffer)
	if err != nil {
		return err
	}

	// Update purged_bytes_read_total
	bytesRead.With(prometheus.Labels{"layer": layer}).Add(float64(nbytes))

	status := string(buffer[9:12])

	// Update purged_http_requests_total
	purgeRequests.With(prometheus.Labels{"status": status, "layer": layer}).Inc()
	return nil
}

func connOrFatal(addr string) net.Conn {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	return conn
}

func worker(ch chan string) {
	backendConn := connOrFatal(*backendAddr)
	frontendConn := connOrFatal(*frontendAddr)

	for rawURL := range ch {
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			log.Println("Error parsing", rawURL, err)
			continue
		}

		err = sendPurge(backendConn, parsedURL.Host, parsedURL.Path, "backend")
		if err != nil {
			log.Printf("Error purging backend: %s", err)
		}

		err = sendPurge(frontendConn, parsedURL.Host, parsedURL.Path, "frontend")
		if err != nil {
			log.Printf("Error purging frontend: %s", err)
		}
	}
}

func main() {
	flag.Parse()

	// Serve prometheus metrics under /metrics, profiling under /debug/pprof
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
