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
	Logger            log.Logger
	ServerConfig      config.ServerConfig
	opcuaClient       *opcua.Client
	opcuaMetricsCache []*opcuaMetric
	statsMetricsCache []*metric
	errorDesc         *prometheus.Desc
}

type opcuaMetric struct {
	*metric
	nodeID          string
	nodeReadValueID *ua.ReadValueID
}

type metric struct {
	name       string
	properties *metricProperties
}

type metricProperties struct {
	desc         *prometheus.Desc
	typ          prometheus.ValueType
	labels       map[string]string
	labelsKeys   []string
	labelsValues []string
}

func NewCollector(cfg *CollectorConfig) (*Collector, error) {
	var err error
	c := &Collector{Logger: cfg.Logger, ServerConfig: *cfg.Config.ServerConfig, opcuaClient: client.NewClientFromServerConfig(*cfg.Config.ServerConfig, cfg.Logger)}
	if err = c.opcuaClient.Connect(context.Background()); err != nil {
		c.Logger.Fatal("cannot connect opcua client %v", err)
	}
	c.ReloadMetrics(cfg.Config.MetricsConfig)
	c.statsMetricsCache = append(c.statsMetricsCache,
		newMetric("opcua_scrape_walk_duration_seconds", "Time OPCUA walk/bulkwalk took.", prometheus.GaugeValue, nil),
		newMetric("opcua_scrape_resp_returned", "RESPs returned from walk.", prometheus.GaugeValue, nil),
		newMetric("opcua_scrape_duration_seconds", "Total OPCUA time scrape took (walk and processing).", prometheus.GaugeValue, nil),
		newMetric("opcua_client_read_duration_seconds", "Time OPCUA to reconnect took.", prometheus.GaugeValue, nil),
	)
	c.errorDesc = prometheus.NewDesc("opcua_error", "error scraping target", nil, nil)
	return c, nil
}

func (c *Collector) ReloadMetrics(cfg *config.MetricsConfig) {
	c.loadMetricsCache(cfg)
}

func (c Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, metric := range c.opcuaMetricsCache {
		ch <- metric.properties.desc
	}
	for _, metric := range c.statsMetricsCache {
		ch <- metric.properties.desc
	}
}

func (c Collector) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()

	opcuaResponse, readDuration, err := c.scrapeTarget()
	if err != nil {
		c.Logger.Info("error scraping target : %s", err)
		ch <- prometheus.NewInvalidMetric(c.errorDesc, err)
		return
	}
	walkDuration := time.Since(start).Seconds()

	for idx, opcuaMetric := range c.opcuaMetricsCache {
		value, err := c.getOpcuaValueFromIndex(opcuaResponse, idx)
		if err != nil {
			ch <- c.getErrorMetric(opcuaMetric.metric, err)
		} else {
			ch <- c.getMetricWithValue(opcuaMetric.metric, value)
		}
	}
	for _, metric := range c.statsMetricsCache {
		var value float64
		switch metric.name {
		case "opcua_client_read_duration_seconds":
			value = readDuration
		case "opcua_scrape_walk_duration_seconds":
			value = walkDuration
		case "opcua_scrape_resp_returned":
			value = float64(len(opcuaResponse.Results))
		case "opcua_scrape_duration_seconds":
			value = time.Since(start).Seconds()
		}
		ch <- c.getMetricWithValue(metric, value)
	}
}

func (c Collector) getErrorMetric(m *metric, err error) prometheus.Metric {
	return prometheus.NewInvalidMetric(c.errorDesc, fmt.Errorf("error for metric %s with labels %v (%w)", m.name, m.properties.labels, err))
}

func (c Collector) getMetricWithValue(m *metric, value float64) prometheus.Metric {
	metric, err := prometheus.NewConstMetric(m.properties.desc, m.properties.typ, value, m.properties.labelsValues...)
	if err != nil {
		return c.getErrorMetric(m, err)
	}
	return metric
}

func (c *Collector) loadMetricsCache(cfg *config.MetricsConfig) error {
	var mm []*opcuaMetric
	for _, m := range cfg.Metrics {
		uaNodeID, err := ua.ParseNodeID(m.NodeID)
		if err != nil {
			return fmt.Errorf("invalid node id: %v", err)
		}
		mm = append(mm, &opcuaMetric{
			nodeID:          m.NodeID,
			nodeReadValueID: &ua.ReadValueID{NodeID: uaNodeID},
			metric:          newMetric(m.Name, m.Help, getMetricValueType(m.Type), m.Labels),
		})
	}
	c.opcuaMetricsCache = mm
	return nil
}

func newMetric(name string, help string, typ prometheus.ValueType, labels map[string]string) *metric {
	var keys, values []string
	for k, v := range labels {
		keys = append(keys, k)
		values = append(values, v)
	}
	return &metric{
		name: name,
		properties: &metricProperties{
			desc:         prometheus.NewDesc(name, help, keys, nil),
			typ:          typ,
			labels:       labels,
			labelsKeys:   keys,
			labelsValues: values,
		},
	}
}

func (c *Collector) scrapeTarget() (*ua.ReadResponse, float64, error) {
	var opcuaNodeIDs []*ua.ReadValueID
	for _, metric := range c.opcuaMetricsCache {
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
		return nil, -1, err
	}
	return resp, time.Since(start).Seconds(), nil
}

func (c *Collector) getOpcuaValueFromIndex(opcuaResponse *ua.ReadResponse, idx int) (float64, error) {
	r := opcuaResponse.Results[idx]
	if r.Status != ua.StatusOK {
		return -1, fmt.Errorf("invalid status %v", r.Status)
	}
	return r.Value.Float(), nil
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
