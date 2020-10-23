package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gopcua/opcua/debug"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/skilld-labs/telemetry-opcua-exporter/collector"
	"github.com/skilld-labs/telemetry-opcua-exporter/config"
	"github.com/skilld-labs/telemetry-opcua-exporter/log"
	"github.com/skilld-labs/telemetry-opcua-exporter/log/jsonlog"
)

var (
	opcuaDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "opcua_collection_duration_seconds",
			Help: "Duration of collections by the OPCUA exporter",
		},
		[]string{"opcua"},
	)
	opcuaRequestErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "opcua_request_errors_total",
			Help: "Errors in requests to the OPCUA exporter",
		},
	)
	opcuaUnexpectedRequestType = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "opcua_unexpected_resp_type_total",
			Help: "Unexpected Go types in a RESP.",
		},
	)
	sc = &SafeConfig{
		C: &config.Config{},
	}
	reloadCh                   chan chan error
	registry                   *prometheus.Registry
	prometheusGoCollector      = prometheus.NewGoCollector()
	prometheusProcessCollector = prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{})
)

func init() {
	registry = prometheus.NewRegistry()
	registry.MustRegister(opcuaDuration, opcuaRequestErrors, version.NewCollector("telemetry_opcua_exporter"), opcuaUnexpectedRequestType)
	registry.MustRegister(prometheusGoCollector, prometheusProcessCollector)
	reloadCh = make(chan chan error)
}

func main() {
	bindAddress := flag.String("bindAddress", ":4242", "Address to listen on for web interface")
	configPath := flag.String("config", "opcua.yaml", "Path to configuration file")
	endpoint := flag.String("endpoint", "", "OPC UA Endpoint URL")
	certPath := flag.String("cert", "", "Path to certificate file")
	keyPath := flag.String("key", "", "Path to PEM Private Key file")
	secMode := flag.String("sec-mode", "auto", "Security Mode: one of None, Sign, SignAndEncrypt")
	secPolicy := flag.String("sec-policy", "None", "Security Policy URL or one of None, Basic128Rsa15, Basic256, Basic256Sha256")
	authMode := flag.String("auth-mode", "Anonymous", "Authentication Mode: one of Anonymous, UserName, Certificate")
	username := flag.String("username", "", "Username to use in auth-mode UserName")
	password := flag.String("password", "", "Password to use in auth-mode UserName")
	verbosity := flag.String("verbosity", "", "Log verbosity (debug/info/warn/error/fatal)")

	flag.Parse()

	if *verbosity == "debug" {
		debug.Enable = true
	}
	logger := jsonlog.NewLogger(&log.LoggerConfiguration{})
	logger.SetVerbosity(*verbosity)
	logger.Info("starting telemetry-opcua-exporter")

	c, err := config.NewConfig(*endpoint, *certPath, *keyPath, *secMode, *secPolicy, *authMode, *username, *password, *configPath)
	if err != nil {
		logger.Err("error parsing config file :%v", err)
		os.Exit(1)
	}
	sc.SetConfig(c)
	reloadConfigOnChannel(logger, *configPath)
	reloadConfigOnSignal(logger)

	metricsCollector, err := collector.NewCollector(&collector.CollectorConfig{Config: sc.GetConfig(), Logger: logger})
	if err != nil {
		logger.Err("error while initializing collector : %v", err)
	}

	if err = registry.Register(*metricsCollector); err != nil {
		logger.Err("error while registering metrics collector : %v", err)
	}

	http.HandleFunc("/metrics", metricsHandler(logger))
	http.HandleFunc("/config", configHandler(sc, logger))

	http.HandleFunc("/config/reload", reloadConfigHandler(logger, metricsCollector, *configPath, false))
	http.HandleFunc("/config/update", reloadConfigHandler(logger, metricsCollector, *configPath, true))

	logger.Info("listening on address: %s", *bindAddress)
	if err = http.ListenAndServe(*bindAddress, nil); err != nil {
		logger.Err("error starting HTTP server: %v", err)
		os.Exit(1)
	}
}

func configHandler(sc *SafeConfig, logger log.Logger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := sc.GetMetricsConfig().Serialize()
		if err != nil {
			logger.Err("error marshaling configuration: %v", err)
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write(c)
	}
}

func metricsHandler(logger log.Logger) func(w http.ResponseWriter, r *http.Request) {
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Info("starting scrape")
		start := time.Now()
		h.ServeHTTP(w, r)
		duration := time.Since(start).Seconds()
		if duration >= float64(8) {
			logger.Warn("%s", duration)
		}
		opcuaDuration.WithLabelValues("opcua").Observe(duration)
		logger.Info("finished scrape, duration_seconds %v", duration)
	}
}

func reloadConfigHandler(logger log.Logger, metricsCollector *collector.Collector, configPath string, updateFromBody bool) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			if updateFromBody {
				body, err := ioutil.ReadAll(r.Body)
				if err != nil {
					logger.Err("Error reading body: %v", err)
					http.Error(w, "can't read body", http.StatusBadRequest)
					return
				}
				if len(body) != 0 {
					config.WriteFile(configPath, body)
				}
				r.Body.Close()
				logger.Info("%s is rewriten", configPath)
			}

			if err := sendReloadChannel(); err != nil {
				http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
			}

			registry.Unregister(*metricsCollector)

			metricsCollector.ReloadMetrics(sc.GetConfig().MetricsConfig)
			err := registry.Register(*metricsCollector)
			if err != nil {
				logger.Err("error while registering metrics collector : %v", err)
			}

		default:
			http.Error(w, "POST method expected", 400)
		}
	}
}

type SafeConfig struct {
	sync.RWMutex
	C *config.Config
}

func (sc *SafeConfig) GetConfig() *config.Config {
	sc.RLock()
	c := sc.C
	sc.RUnlock()
	return c
}

func (sc *SafeConfig) GetMetricsConfig() *config.MetricsConfig {
	sc.RLock()
	c := sc.C.MetricsConfig
	sc.RUnlock()
	return c
}

func (sc *SafeConfig) SetConfig(c *config.Config) {
	sc.Lock()
	sc.C = c
	sc.Unlock()
}

func (sc *SafeConfig) reloadConfig(configPath string) error {
	c := sc.GetConfig()
	if err := c.LoadMetricsConfig(configPath); err != nil {
		return err
	}
	sc.SetConfig(c)
	return nil
}

func sendReloadChannel() error {
	rc := make(chan error)
	reloadCh <- rc
	return <-rc
}

func reloadConfigOnSignal(logger log.Logger) {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				if err := sendReloadChannel(); err != nil {
					logger.Err(err.Error())
				}
			}
		}
	}()
}

func reloadConfigOnChannel(logger log.Logger, configPath string) {
	go func() {
		for {
			select {
			case rc := <-reloadCh:
				if err := sc.reloadConfig(configPath); err != nil {
					logger.Err("error reloading config: %v", err)
					rc <- err
				} else {
					logger.Info("config file was reloaded")
					rc <- nil
				}
			}
		}
	}()
}
