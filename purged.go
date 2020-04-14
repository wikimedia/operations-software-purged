package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type PurgeClient interface {
	Send(host, uri string) (string, error) // return status code and error (if any)
}

type TCPPurger struct {
	conn     net.Conn
	destAddr string
}

type HTTPPurger struct {
	client   http.Client
	destAddr string
}

const (
	purgeReq           = "PURGE %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: purged\r\n\r\n"
	connectionAttempts = 16
)

var (
	frontendAddr     = flag.String("frontend_addr", "127.0.0.1:80", "Cache frontend address")
	backendAddr      = flag.String("backend_addr", "127.0.0.1:3128", "Cache backend address")
	mcastAddrs       = flag.String("mcast_addrs", "239.128.0.112:4827,239.128.0.115:4827", "Comma separated list of multicast addresses")
	metricsAddr      = flag.String("prometheus_addr", ":2112", "TCP network address for prometheus metrics")
	nBackendWorkers  = flag.Int("backend_workers", 4, "Number of backend purger goroutines")
	nFrontendWorkers = flag.Int("frontend_workers", 1, "Number of frontend purger goroutines")
	nethttp          = flag.Bool("nethttp", false, "Use net/http (default false)")
	purgeRequests    = promauto.NewCounterVec(prometheus.CounterOpts{
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
)

func connOrFatal(addr string) net.Conn {
	for i := 7; i < connectionAttempts; i++ {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			return conn
		} else {
			connectRetryTime := math.Pow(2, float64(i))
			log.Printf("Error connecting to %v: %v. Reconnecting in %v milliseconds\n", addr, err, connectRetryTime)
			time.Sleep(time.Duration(connectRetryTime) * time.Millisecond)
		}
	}

	log.Fatal("Giving up connecting to ", addr)
	return nil
}

func NewTCPPurger(addr string) *TCPPurger {
	return &TCPPurger{conn: connOrFatal(addr), destAddr: addr}
}

func (p *TCPPurger) Send(host, uri string) (string, error) {
	buffer := make([]byte, 4096)

	for {
		_, err := fmt.Fprintf(p.conn, purgeReq, uri, host)
		if err == nil {
			break
		} else {
			// try reconnecting on any sort of request errors till we finally
			// manage to send our request. Give up with a fatal only after
			// connectionAttempts is reached.
			p.conn = connOrFatal(p.destAddr)
		}
	}

	_, err := p.conn.Read(buffer)
	if err != nil {
		// on response errors, try reconnecting but other than that do not
		// bother too much reading the response status code.
		p.conn = connOrFatal(p.destAddr)
		return "", err
	} else {
		status := string(buffer[9:12])
		return status, nil
	}
}

func NewHTTPPurger(addr string) *HTTPPurger {
	// Override DefaultTransport and keep it simple: establish one TCP
	// connection to addr for each HTTPPurger
	var netTransport = &http.Transport{}
	client := http.Client{Transport: netTransport}
	return &HTTPPurger{client: client, destAddr: addr}
}

func (p *HTTPPurger) Send(host, uri string) (string, error) {
	// Create request
	req, err := http.NewRequest("PURGE", "http://"+p.destAddr+uri, nil)
	if err != nil {
		return "", err
	}
	req.Host = host

	// Send request
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}

	// Read response
	defer resp.Body.Close()

	_, err = io.Copy(ioutil.Discard, resp.Body)
	if err != nil {
		return "", err
	}

	status := strconv.Itoa(resp.StatusCode)

	return status, err
}

func backendWorker(chin chan string, chout chan string) {
	var backend PurgeClient

	if *nethttp {
		backend = NewHTTPPurger(*backendAddr)
	} else {
		backend = NewTCPPurger(*backendAddr)
	}

	for rawURL := range chin {
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			log.Println("Error parsing", rawURL, err)
			continue
		}

		status, err := backend.Send(parsedURL.Host, parsedURL.Path)
		if err != nil {
			log.Printf("Error purging backend: %s", err)
		}
		// Update purged_http_requests_total
		purgeRequests.With(prometheus.Labels{"status": status, "layer": "backend"}).Inc()

		// Send URL to frontend workers
		chout <- rawURL
	}
}

func frontendWorker(chin chan string) {
	// XXX: code copy-paster from backend, refactor
	var frontend PurgeClient

	if *nethttp {
		frontend = NewHTTPPurger(*frontendAddr)
	} else {
		frontend = NewTCPPurger(*frontendAddr)
	}

	for rawURL := range chin {
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			log.Println("Error parsing", rawURL, err)
			continue
		}

		status, err := frontend.Send(parsedURL.Host, parsedURL.Path)
		if err != nil {
			log.Printf("Error purging frontend: %s", err)
		}
		// Update purged_http_requests_total
		purgeRequests.With(prometheus.Labels{"status": status, "layer": "frontend"}).Inc()
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
	chBackend := make(chan string, 1000000)
	// Begin producing URLs to chBackend for consumption by backend workers
	go pr.Read(chBackend)

	// channel for consumption by frontend workers
	chFrontend := make(chan string, 1000000)
	for i := 0; i < *nBackendWorkers; i++ {
		go backendWorker(chBackend, chFrontend)
	}

	for i := 0; i < *nFrontendWorkers; i++ {
		go frontendWorker(chFrontend)
	}

	log.Printf("Process purged started with %d backend and %d frontend workers. Metrics at %s/metrics\n", *nBackendWorkers, *nFrontendWorkers, *metricsAddr)

	for {
		// Update purged_backlog metric
		time.Sleep(1000 * time.Millisecond)
		backlog.Set(float64(len(chBackend)))
	}
}
