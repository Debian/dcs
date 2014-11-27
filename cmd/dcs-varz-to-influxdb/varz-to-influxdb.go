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

	shortHost  string
	lastUptime int64
	baseValues = make(map[string]float64)
	lastValues = make(map[string]float64)
)

func scrapeVarz(db *influxdb.Client) {
	resp, err := http.Get(*targetURL)
	if err != nil {
		log.Printf("Error scraping %q: %v\n", *targetURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("HTTP Error scraping %q: %v\n", *targetURL, err)
		return
	}

	targetRestarted := false

	if uptime := resp.Header.Get("X-Uptime"); uptime != "" {
		uptime64, err := strconv.ParseInt(uptime, 10, 0)
		if err != nil {
			log.Fatal("Invalid uptime header %q\n", uptime)
		}
		if uptime64 < lastUptime {
			targetRestarted = true
		}
		lastUptime = uptime64
	}

	suffix := "." + shortHost

	var seriesBatch []*influxdb.Series
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), " ", 2)
		name := parts[0] + suffix

		var points []interface{}
		columns := []string{"value", "seconds-since-now", "counter"}
		value, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			points = []interface{}{
				parts[1],
				"",
				"",
			}
		} else {
			if targetRestarted {
				log.Printf("restart detected. setting base for %q from %f to %f\n", name, baseValues[name], lastValues[name])
				baseValues[name] = lastValues[name]
			}
			counter := baseValues[name] + value
			lastValues[name] = counter
			columns = []string{"value", "seconds-since-now", "counter"}
			points = []interface{}{
				value,
				time.Now().Sub(time.Unix(int64(value), 0)).Seconds(),
				counter,
			}
		}

		series := influxdb.Series{
			Name:    parts[0] + suffix,
			Columns: columns,
			Points:  [][]interface{}{points},
		}
		seriesBatch = append(seriesBatch, &series)
	}

	var points []interface{}
	if targetRestarted {
		points = []interface{}{1, "", ""}
	} else {
		points = []interface{}{0, "", ""}
	}
	series := influxdb.Series{
		Name:    "restart-detected" + suffix,
		Columns: []string{"value", "seconds-since-now", "counter"},
		Points:  [][]interface{}{points},
	}
	seriesBatch = append(seriesBatch, &series)

	if err := db.WriteSeries(seriesBatch); err != nil {
		log.Fatal(err)
	}

	log.Printf("Inserted %d timeseries into InfluxDB", len(seriesBatch))
}

func main() {
	flag.Parse()

	// parse the hostname and append that to the varz name,
	// e.g. num-goroutines becomes num-goroutines.int-dcs-web
	url, err := url.Parse(*targetURL)
	if err != nil {
		log.Fatal(err)
	}
	shortHost = strings.Split(url.Host, ".")[0]

	db, err := influxdb.NewClient(&influxdb.ClientConfig{
		Host:     *influxDBHost,
		Database: *influxDBDatabase,
		Username: *influxDBUsername,
		Password: *influxDBPassword,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Get the last value for each time series for the target host so that we
	// can continue old counters.
	allSeries, err := db.Query(`select counter, (counter - value) AS base from /.*.` + shortHost + `/ where counter > 0 limit 1;`)
	if err != nil {
		log.Printf("restoring last/base failed: %v\n", err)
	}
	for _, series := range allSeries {
		log.Printf("name = %s with %d points\n", series.Name, len(series.Points))
		if len(series.Points) != 1 {
			continue
		}
		values := series.Points[0]
		log.Printf("values = %+v\n", values)
		if len(values) != 4 {
			continue
		}
		if v, ok := values[2].(float64); ok {
			lastValues[series.Name] = v
			log.Printf("Restored last[%q] to %f\n", series.Name, lastValues[series.Name])
		}
		if v, ok := values[3].(float64); ok {
			baseValues[series.Name] = v
			log.Printf("Restored base[%q] to %f\n", series.Name, baseValues[series.Name])
		}
	}

	for {
		scrapeVarz(db)
		time.Sleep(time.Duration(*scrapeInterval) * time.Second)
	}
}
