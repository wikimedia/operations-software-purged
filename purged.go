// Copyright (C) 2020 Emanuele Rocca <ema@wikimedia.org>
// Copyright (C) 2020 Wikimedia Foundation, Inc.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"errors"
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
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
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
	sendAttempts       = 10
	bufferLen          = 1000000
	statusLabel        = "status"
	layerLabel         = "layer"
	backendValue       = "backend"
	frontendValue      = "frontend"
	typeLabel          = "type"
)

var (
	frontendAddr     = flag.String("frontend_addr", "127.0.0.1:80", "Cache frontend address")
	backendAddr      = flag.String("backend_addr", "127.0.0.1:3128", "Cache backend address")
	mcastAddrs       = flag.String("mcast_addrs", "239.128.0.112,239.128.0.115", "Comma separated list of multicast addresses")
	mcastBufSize     = flag.Int("mcast_bufsize", 16777216, "Multicast reader kernel buffer size")
	metricsAddr      = flag.String("prometheus_addr", ":2112", "TCP network address for prometheus metrics")
	hostRegex        = flag.String("host_regex", "", "Regex filter for valid purge hostnames (default unfiltered)")
	nBackendWorkers  = flag.Int("backend_workers", 4, "Number of backend purger goroutines")
	nFrontendWorkers = flag.Int("frontend_workers", 1, "Number of frontend purger goroutines")
	frontendDelay    = flag.Int("frontend_delay", 1000, "Delay in milliseconds between backend and frontend PURGE")
	nethttp          = flag.Bool("nethttp", false, "Use net/http (default false)")
	kafkaTopics      = flag.String("topics", "", "Optional, comma-separated list of kafka topics to listen to.")
	kafkaConfigFile  = flag.String("kafkaConfig", "/etc/purgedkafka.conf", "Kafka configuration file")
	purgeRequests    = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_http_requests_total",
		Help: "Total number of HTTP PURGE sent by status code",
	}, []string{
		statusLabel,
		layerLabel,
	})
	tcpErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "purged_tcp_errors_total",
		Help: "Total number of TCP read/write errors",
	}, []string{
		typeLabel,
	})
	backlog = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "purged_backlog",
		Help: "Number of messages still to be processed by backend and frontend workers",
	}, []string{
		layerLabel,
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

	var errType string

	for i := 0; i < sendAttempts; i++ {
		_, err := fmt.Fprintf(p.conn, purgeReq, uri, host)
		if err != nil {
			// OpErrors are common (eg: broken pipe), we don't need to log
			// them. Incrementing the relevant metric is enough.
			if opErr, ok := err.(*net.OpError); ok {
				errType = opErr.Err.Error()
			} else {
				errType = "write"
				log.Printf("Write error: %v\n", err)
			}

			tcpErrors.With(prometheus.Labels{typeLabel: errType}).Inc()
			p.conn = connOrFatal(p.destAddr)
			continue
		}

		_, err = p.conn.Read(buffer)
		if err != nil {
			// EOF errors are common (connection closed), we don't need to log
			// them. Incrementing the relevant metric is enough.
			if err == io.EOF {
				errType = "EOF"
			} else {
				errType = "read"
				log.Printf("Read error: %v\n", err)
			}
			tcpErrors.With(prometheus.Labels{typeLabel: errType}).Inc()
			p.conn = connOrFatal(p.destAddr)
			continue
		} else {
			// Both write and read were successful
			status := string(buffer[9:12])
			return status, nil
		}
	}

	return "", errors.New(fmt.Sprintf("Failed purging %s (Host: %s) after %d attempts", uri, host, sendAttempts))
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

// delayedPurge sends the given url on the given channel. The reason for
// returning a function here is that we want to use time.AfterFunc, which takes
// a function as an argument -- just func(), without parameters.
func delayedPurge(chout chan url.URL, toPurge url.URL) func() {
	return func() {
		chout <- toPurge
	}
}

func backendWorker(addr string, chin chan string, chout chan url.URL, re *regexp.Regexp) {
	var backend PurgeClient

	if *nethttp {
		backend = NewHTTPPurger(addr)
	} else {
		backend = NewTCPPurger(addr)
	}

	for rawURL := range chin {
		parsedURL, err := url.Parse(rawURL)
		if err != nil {
			log.Println("Error parsing", rawURL, err)
			continue
		}

		if re != nil && !re.Match([]byte(parsedURL.Host)) {
			continue
		}

		status, err := backend.Send(parsedURL.Host, parsedURL.RequestURI())
		if err != nil {
			log.Printf("Error purging backend: %s", err)
		}
		// Update purged_http_requests_total
		purgeRequests.With(prometheus.Labels{statusLabel: status, layerLabel: backendValue}).Inc()

		// Send parsed URL to frontend workers
		time.AfterFunc(time.Duration(*frontendDelay)*time.Millisecond, delayedPurge(chout, *parsedURL))
	}
}

func frontendWorker(addr string, chin chan url.URL) {
	var frontend PurgeClient

	if *nethttp {
		frontend = NewHTTPPurger(addr)
	} else {
		frontend = NewTCPPurger(addr)
	}

	for parsedURL := range chin {
		status, err := frontend.Send(parsedURL.Host, parsedURL.RequestURI())
		if err != nil {
			log.Printf("Error purging frontend: %s", err)
		}
		// Update purged_http_requests_total
		purgeRequests.With(prometheus.Labels{statusLabel: status, layerLabel: frontendValue}).Inc()
	}
}

func startWorkers(beAddr, feAddr string, chBackend chan string, chFrontend chan url.URL, re *regexp.Regexp) {
	for i := 0; i < *nBackendWorkers; i++ {
		go backendWorker(beAddr, chBackend, chFrontend, re)
	}

	for i := 0; i < *nFrontendWorkers; i++ {
		go frontendWorker(feAddr, chFrontend)
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
	pr := MultiCastReader{maxDatagramSize: 4096, mcastAddrs: *mcastAddrs, kbufSize: *mcastBufSize}

	chBackend := make(chan string, bufferLen)

	// If we're also listening on kafka, setup the kafka reader too
	if *kafkaTopics != "" {
		// Given kafka has an eventloop, we need to reliably signal it that the work is done when exiting
		sigchan := make(chan os.Signal, 1)
		// We stop execution on signals SIGTERM and SIGINT
		signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
		// Channel to notify the goroutines.
		done := make(chan struct{})
		go func() {
			for sig := range sigchan {
				log.Printf("Exiting on signal %v", sig)
				// Send kafka a message telling it to stop
				m := struct{}{}
				done <- m
				// Wait for kafka to communicate back by closing the channel
				<-done
				os.Exit(0)
			}
		}()

		log.Printf("Listening for topics %s", *kafkaTopics)
		topics := strings.Split(*kafkaTopics, ",")
		kafkaPr, err := NewKafkaReader(*kafkaConfigFile, topics, done)
		if err != nil {
			log.Fatal(err)
		}
		go func(c chan string) {
			kafkaPr.Read(c)
			log.Println("Kafka connection stopped")
		}(chBackend)
	}

	// Begin producing URLs to chBackend for consumption by backend workers
	go pr.Read(chBackend)

	// channel for consumption by frontend workers
	chFrontend := make(chan url.URL, bufferLen)

	// Start backend and frontend workers
	var re *regexp.Regexp
	if *hostRegex != "" {
		re = regexp.MustCompile(*hostRegex)
	} else {
		re = nil
	}
	startWorkers(*backendAddr, *frontendAddr, chBackend, chFrontend, re)

	log.Printf("Process purged started with %d backend and %d frontend workers. Metrics at %s/metrics\n", *nBackendWorkers, *nFrontendWorkers, *metricsAddr)

	for {
		// Update purged_backlog metric
		time.Sleep(1000 * time.Millisecond)

		backlog.With(prometheus.Labels{layerLabel: backendValue}).Set(float64(len(chBackend)))
		backlog.With(prometheus.Labels{layerLabel: frontendValue}).Set(float64(len(chFrontend)))
	}
}
