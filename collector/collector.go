package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/skilld-labs/telemetry-opcua-exporter/client"
	"github.com/skilld-labs/telemetry-opcua-exporter/config"
	"github.com/skilld-labs/telemetry-opcua-exporter/log"
)

type CollectorConfig struct {
	Config *config.Config
	Logger log.Logger
}

type Collector struct {
	Logger       log.Logger
	ServerConfig config.ServerConfig
	opcuaClient  *opcua.Client
	metricsCache []metric
}

type metric struct {
	name            string
	labels          map[string]string
	labelsKeys      []string
	labelsValues    []string
	nodeID          string
	nodeReadValueID *ua.ReadValueID
	promDesc        *prometheus.Desc
	typ             prometheus.ValueType
}

func NewCollector(cfg *CollectorConfig) (*Collector, error) {
	c := &Collector{Logger: cfg.Logger, ServerConfig: *cfg.Config.ServerConfig}
	c.opcuaClient = client.NewClientFromServerConfig(*cfg.Config.ServerConfig, cfg.Logger)
	if err := c.opcuaClient.Connect(context.Background()); err != nil {
		c.Logger.Fatal("cannot connect opcua client %v", err)
	}
	c.ReloadMetrics(cfg.Config.MetricsConfig)
	return c, nil
}

func (c *Collector) ReloadMetrics(cfg *config.MetricsConfig) {
	c.loadMetricsCache(cfg)
}

func (c Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range c.metricsCache {
		ch <- metric.promDesc
	}
}

func (c Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()
	opcuaResponse, promMetrics, err := c.scrapeTarget()
	if err != nil {
		c.Logger.Info("error scraping target : %s", err)
		ch <- prometheus.NewInvalidMetric(prometheus.NewDesc("opcua_error", "Error scraping target", nil, nil), err)
		return
	}
	for _, metrics := range promMetrics {
		ch <- metrics
	}
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("opcua_scrape_walk_duration_seconds", "Time OPCUA walk/bulkwalk took.", nil, nil),
		prometheus.GaugeValue,
		time.Since(start).Seconds())
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("opcua_scrape_resp_returned", "RESPs returned from walk.", nil, nil),
		prometheus.GaugeValue,
		float64(len(opcuaResponse.Results)))

	for _, opcuaMetric := range c.opcuaToPrometheusMetrics(opcuaResponse) {
		ch <- opcuaMetric
	}

	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("opcua_scrape_duration_seconds", "Total OPCUA time scrape took (walk and processing).", nil, nil),
		prometheus.GaugeValue,
		time.Since(start).Seconds())
}

func (c *Collector) loadMetricsCache(cfg *config.MetricsConfig) error {
	var result []metric
	for _, m := range cfg.Metrics {
		uaNodeID, err := ua.ParseNodeID(m.NodeID)
		if err != nil {
			return fmt.Errorf("invalid node id: %v", err)
		}
		var keys, values []string
		for k, v := range m.Labels {
			keys = append(keys, k)
			values = append(values, v)
		}
		result = append(result, metric{
			name:            m.Name,
			labels:          m.Labels,
			labelsKeys:      keys,
			labelsValues:    values,
			nodeID:          m.NodeID,
			nodeReadValueID: &ua.ReadValueID{NodeID: uaNodeID},
			promDesc:        prometheus.NewDesc(m.Name, m.Help, keys, nil),
			typ:             getMetricValueType(m.Type)})
	}
	c.metricsCache = result
	return nil
}

func (c *Collector) scrapeTarget() (*ua.ReadResponse, []prometheus.Metric, error) {
	var opcuaNodeIDs []*ua.ReadValueID
	var promMetrics []prometheus.Metric
	for _, metric := range c.metricsCache {
		opcuaNodeIDs = append(opcuaNodeIDs, metric.nodeReadValueID)
	}

	req := &ua.ReadRequest{
		MaxAge:             2000,
		NodesToRead:        opcuaNodeIDs,
		TimestampsToReturn: ua.TimestampsToReturnBoth,
	}
	start := time.Now()
	resp, err := c.opcuaClient.Read(req)
	if err != nil {
		c.Logger.Err("read failed: %s", err)
		return nil, nil, err
	}
	promMetrics = append(promMetrics, prometheus.MustNewConstMetric(
		prometheus.NewDesc("opcua_client_read_duration_seconds", "Time OPCUA to reconnect took.", nil, nil),
		prometheus.GaugeValue,
		time.Since(start).Seconds()))

	return resp, promMetrics, nil
}

func (c *Collector) opcuaToPrometheusMetrics(opcuaResponse *ua.ReadResponse) []prometheus.Metric {
	var opcuaMetrics []prometheus.Metric
	for idx, m := range c.metricsCache {
		var sample prometheus.Metric
		var err error
		opcuaResult := opcuaResponse.Results[idx]

		switch status := opcuaResult.Status; {
		case status != ua.StatusOK:
			sample = prometheus.NewInvalidMetric(prometheus.NewDesc("opcua_error", "Error calling NewConstMetric", nil, nil),
				fmt.Errorf("error for metric %s with labels %v: %v", m.name, m.labels, opcuaResult.Status))
		case status == ua.StatusOK:
			if sample, err = prometheus.NewConstMetric(m.promDesc, m.typ, opcuaResult.Value.Float(), m.labelsValues...); err != nil {
				sample = prometheus.NewInvalidMetric(prometheus.NewDesc("opcua_error", "Error calling NewConstMetric", nil, nil),
					fmt.Errorf("error for metric %s with labels %v: %v", m.name, m.labels, err))
			}
		}
		opcuaMetrics = append(opcuaMetrics, sample)
	}
	return opcuaMetrics
}

func getMetricValueType(metricType string) prometheus.ValueType {
	t := prometheus.UntypedValue
	switch metricType {
	case "counter":
		t = prometheus.CounterValue
	case "gauge":
		t = prometheus.GaugeValue
	case "Float", "Double":
		t = prometheus.GaugeValue
	}
	return t
}

func fromMSSToSA(input map[string]string) (result []string) {
	for value := range input {
		result = append(result, value)
	}
	return result
}
