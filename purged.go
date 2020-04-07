package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"time"
)

var (
	frontendURL = flag.String("frontend_url", "http://127.0.0.1:80", "Cache frontend URL")
	backendURL  = flag.String("backend_url", "http://127.0.0.1:3128", "Cache backend URL")
	purgeErrors = 0
)

func sendPurge(client *http.Client, baseUrl, path, host string) error {
	rawurl := baseUrl + path
	req, err := http.NewRequest("PURGE", rawurl, nil)
	if err != nil {
		log.Printf("Failed creating request for url %s\n", rawurl)
		return err
	}

	req.Header.Add("Host", host)
	resp, err := client.Do(req)
	defer resp.Body.Close()

	if err != nil {
		log.Printf("Failed sending request to Host: %s -> %s: %s\n", host, rawurl, err)
		return err
	}

	_, err = io.Copy(ioutil.Discard, resp.Body)
	if err != nil {
		log.Println(err)
		purgeErrors++
	}

	if resp.StatusCode != 204 && resp.StatusCode != 404 {
		purgeErrors++
	}
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

	// Setup reader
	pr := MultiCastReader{maxDatagramSize: 4096, mcastAddr: "239.128.0.112:4827"}
	ch := make(chan string, 1000000)
	// Begin producing URLs to ch
	go pr.Read(ch)

	for i := 0; i < runtime.NumCPU(); i++ {
		go worker(ch)
	}

	for {
		time.Sleep(1000 * time.Millisecond)
		log.Println("backlog", len(ch), "errors", purgeErrors)
	}
}
