package main

import (
	"crypto/tls"
	"flag"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/matt-deboer/go-marathon"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

var (
	listenAddress = flag.String(
		"web.listen-address", ":9088",
		"Address to listen on for web interface and telemetry.")

	metricsPath = flag.String(
		"web.telemetry-path", "/metrics",
		"Path under which to expose metrics.")

	marathonUri = flag.String(
		"marathon.uri", "http://marathon.mesos:8080",
		"URI of Marathon")

	labelsToScrape = flag.String(
		"app.labels", "",
		"Labels to scrape from each app")
)

func getUserPass(uri *url.URL) (string, string) {
	if uri.User != nil {
		if pass, ok := uri.User.Password(); ok {
			return uri.User.Username(), pass
		}
		return "", ""
	}
	return os.Getenv("MARATHON_USERNAME"), os.Getenv("MARATHON_PASSWORD")
}

func marathonConnect(uri *url.URL) error {
	config := marathon.NewDefaultConfig()
	config.URL = uri.String()

	user, pass := getUserPass(uri)
	if user != "" && pass != "" {
		config.HTTPBasicAuthUser = user
		config.HTTPBasicPassword = pass
	}

	config.HTTPClient = &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout: 10 * time.Second,
			}).Dial,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	log.Debugf("Connecting to Marathon: %s | username=%s | len(password)=%d", stripAuthority(uri), user, utf8.RuneCountInString(pass))
	client, err := marathon.NewClient(config)
	if err != nil {
		return err
	}

	info, err := client.Info()
	if err != nil {
		return err
	}

	log.Debugf("Connected to Marathon! Name=%s, Version=%s\n", info.Name, info.Version)
	return nil
}

func stripAuthority(u *url.URL) string {
	_, passSet := u.User.Password()
	if passSet {
		return strings.Replace(u.String(), u.User.String()+"@", "", 1)
	}
	return u.String()
}

func main() {
	flag.Parse()
	uri, err := url.Parse(*marathonUri)
	if err != nil {
		log.Fatal(err)
	}

	retryTimeout := time.Duration(10 * time.Second)
	for {
		err := marathonConnect(uri)
		if err == nil {
			break
		}

		log.Debugf("Problem connecting to Marathon: %v", err)
		log.Infof("Couldn't connect to Marathon! Trying again in %v", retryTimeout)
		time.Sleep(retryTimeout)
	}

	labels := strings.Split(*labelsToScrape, ",")
	exporter := NewExporter(&scraper{uri}, defaultNamespace, labels)
	prometheus.MustRegister(exporter)

	http.Handle(*metricsPath, prometheus.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
             <head><title>Marathon Exporter</title></head>
             <body>
             <h1>Marathon Exporter</h1>
             <p><a href='` + *metricsPath + `'>Metrics</a></p>
             </body>
             </html>`))
	})

	log.Info("Starting Server: ", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
