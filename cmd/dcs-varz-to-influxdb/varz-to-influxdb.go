// Scrapes /varz periodically and pushes the timeseries to InfluxDB.
//
// You are expected to run one process per target exposing /varz, because
// scraping happens in a blocking fashion. The small scale of DCS makes this
// design feasible.
package main

import (
	"bufio"
	"flag"
	"github.com/influxdb/influxdb-go"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	targetURL = flag.String("target_url",
		"http://localhost/varz",
		"HTTP URL to the /varz page you want to scrape")
	scrapeInterval = flag.Int("scrape_interval",
		30,
		"Interval (in seconds) for when to scrape the target.")
	influxDBHost = flag.String("influx_db_host",
		"localhost:8086",
		"host:port of the InfluxDB to store time series in")
	influxDBDatabase = flag.String("influx_db_database",
		"dcs",
		"InfluxDB database name")
	influxDBUsername = flag.String("influx_db_username",
		"root",
		"InfluxDB username")
	influxDBPassword = flag.String("influx_db_password",
		"root",
		"InfluxDB password")
)

func scrapeVarz(db *influxdb.Client) {
	resp, err := http.Get(*targetURL)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatal(err)
	}

	// parse the hostname and append that to the varz name,
	// e.g. num-goroutines becomes num-goroutines.int-dcs-web
	url, err := url.Parse(*targetURL)
	if err != nil {
		log.Fatal(err)
	}
	suffix := "." + strings.Split(url.Host, ".")[0]

	var seriesBatch []*influxdb.Series
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " ", 2)

		var points []interface{}
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			points = []interface{}{
				parts[1],
			}
		} else {
			points = []interface{}{
				value,
			}
		}

		series := influxdb.Series{
			Name:    parts[0] + suffix,
			Columns: []string{"value"},
			Points:  [][]interface{}{points},
		}
		seriesBatch = append(seriesBatch, &series)
	}

	if err := db.WriteSeries(seriesBatch); err != nil {
		log.Fatal(err)
	}

	log.Printf("Inserted %d timeseries into InfluxDB", len(seriesBatch))
}

func main() {
	flag.Parse()

	db, err := influxdb.NewClient(&influxdb.ClientConfig{
		Host:     *influxDBHost,
		Database: *influxDBDatabase,
		Username: *influxDBUsername,
		Password: *influxDBPassword,
	})
	if err != nil {
		log.Fatal(err)
	}

	for {
		scrapeVarz(db)
		time.Sleep(time.Duration(*scrapeInterval) * time.Second)
	}
}
