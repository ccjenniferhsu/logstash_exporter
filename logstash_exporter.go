package main

import (
	"net/http"
	"log"
	_ "net/http/pprof"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/ccjenniferhsu/logstash_exporter/collector"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	scrapeDurations = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: collector.Namespace,
			Subsystem: "exporter",
			Name:      "scrape_duration_seconds",
			Help:      "logstash_exporter: Duration of a scrape job.",
		},
		[]string{"collector", "result"},
	)
)

// LogstashCollector collector type
type LogstashCollector struct {
	collectors map[string]collector.Collector
}

// NewLogstashCollector register a logstash collector
func NewLogstashCollector(logstashEndpoint string) (*LogstashCollector, error) {
	nodeStatsCollector, err := collector.NewNodeStatsCollector(logstashEndpoint)
	if err != nil {
		log.Fatalf("Cannot register a new collector: %v\n", err)
	}

	nodeInfoCollector, err := collector.NewNodeInfoCollector(logstashEndpoint)
	if err != nil {
		log.Fatalf("Cannot register a new collector: %v\n", err)
	}

	return &LogstashCollector{
		collectors: map[string]collector.Collector{
			"node": nodeStatsCollector,
			"info": nodeInfoCollector,
		},
	}, nil
}

func listen(exporterBindAddress string) {
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/metrics", http.StatusMovedPermanently)
	})

	log.Println("Starting server on", exporterBindAddress)
	if err := http.ListenAndServe(exporterBindAddress, nil); err != nil {
		log.Fatalf("Cannot start Logstash exporter: %s\n", err)
	}
}

// Describe logstash metrics
func (coll LogstashCollector) Describe(ch chan<- *prometheus.Desc) {
	scrapeDurations.Describe(ch)
}

// Collect logstash metrics
func (coll LogstashCollector) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(coll.collectors))
	for name, c := range coll.collectors {
		go func(name string, c collector.Collector) {
			execute(name, c, ch)
			wg.Done()
		}(name, c)
	}
	wg.Wait()
	scrapeDurations.Collect(ch)
}

func execute(name string, c collector.Collector, ch chan<- prometheus.Metric) {
	begin := time.Now()
	err := c.Collect(ch)
	duration := time.Since(begin)
	var result string

	if err != nil {
		log.Printf("ERROR: %s collector failed after %fs: %s\n", name, duration.Seconds(), err)
		result = "error"
	} else {
		log.Printf("OK: %s collector succeeded after %fs.\n", name, duration.Seconds())
		result = "success"
	}
	scrapeDurations.WithLabelValues(name, result).Observe(duration.Seconds())
}

func init() {
	prometheus.MustRegister(version.NewCollector("logstash_exporter"))
}

func main() {
	var (
		logstashEndpoint    = kingpin.Flag("logstash.endpoint", "The protocol, host and port on which logstash metrics API listens").Default("http://localhost:9600").String()
		exporterBindAddress = kingpin.Flag("web.listen-address", "Address on which to expose metrics and web interface.").Default(":9198").String()
	)

	kingpin.Version(version.Print("logstash_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()

	logstashCollector, err := NewLogstashCollector(*logstashEndpoint)
	if err != nil {
		log.Fatalf("Cannot register a new Logstash Collector: %v", err)
	}

	prometheus.MustRegister(logstashCollector)

	log.Println("Starting Logstash exporter", version.Info())
	log.Println("Build context", version.BuildContext())
	listen(*exporterBindAddress)
}
