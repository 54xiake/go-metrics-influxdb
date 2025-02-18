package influxdb

import (
	"context"
	"fmt"
	"log"
	uurl "net/url"
	"time"

	client "github.com/influxdata/influxdb-client-go/v2"
	"github.com/rcrowley/go-metrics"
)

type reporter struct {
	reg      metrics.Registry
	interval time.Duration
	align    bool
	url      uurl.URL
	bucket   string

	measurement string
	org         string
	token       string
	tags        map[string]string

	client client.Client
}

// InfluxDB starts a InfluxDB reporter which will post the metrics from the given registry at each d interval.
func InfluxDB(ctx context.Context, r metrics.Registry, d time.Duration, url, bucket, measurement, org, token string, align bool) {
	InfluxDBWithTags(ctx, r, d, url, bucket, measurement, org, token, map[string]string{}, align)
}

// InfluxDBWithTags starts a InfluxDB reporter which will post the metrics from the given registry at each d interval with the specified tags
func InfluxDBWithTags(ctx context.Context, r metrics.Registry, d time.Duration, url, bucket, measurement, org, token string, tags map[string]string, align bool) {
	u, err := uurl.Parse(url)
	if err != nil {
		log.Printf("unable to parse InfluxDB url %s. err=%v", url, err)
		return
	}

	rep := &reporter{
		reg:         r,
		interval:    d,
		url:         *u,
		bucket:      bucket,
		measurement: measurement,
		org:         org,
		token:       token,
		tags:        tags,
		align:       align,
	}
	rep.makeClient()

	rep.run(ctx)
}

func (r *reporter) makeClient() {
	r.client = client.NewClient(r.url.String(), r.token)

}

func (r *reporter) run(ctx context.Context) {
	intervalTicker := time.Tick(r.interval)
	pingTicker := time.Tick(time.Second * 5)

	for {
		select {
		case <-intervalTicker:
			if err := r.send(); err != nil {
				log.Printf("unable to send metrics to InfluxDB. err=%v", err)
			}
		case <-pingTicker:
			isReady, err := r.client.Ready(ctx)
			if err != nil || isReady == false {
				log.Printf("got error while sending a ping to InfluxDB, trying to recreate client. err=%v", err)
				r.makeClient()
			}
		}
	}
}

func (r *reporter) send() error {
	writeAPI := r.client.WriteAPI(r.org, r.bucket)

	now := time.Now()
	if r.align {
		now = now.Truncate(r.interval)
	}
	r.reg.Each(func(name string, i interface{}) {

		switch metric := i.(type) {
		case metrics.Counter:
			ms := metric.Snapshot()
			p := client.NewPoint(r.measurement,
				r.tags,
				map[string]interface{}{
					fmt.Sprintf("%s.count", name): ms.Count(),
				},
				now)
			writeAPI.WritePoint(p)
		case metrics.Gauge:
			ms := metric.Snapshot()
			p := client.NewPoint(r.measurement,
				r.tags,
				map[string]interface{}{
					fmt.Sprintf("%s.gauge", name): ms.Value(),
				},
				now)
			writeAPI.WritePoint(p)
		case metrics.GaugeFloat64:
			ms := metric.Snapshot()
			p := client.NewPoint(r.measurement,
				r.tags,
				map[string]interface{}{
					fmt.Sprintf("%s.gauge", name): ms.Value(),
				},
				now)
			writeAPI.WritePoint(p)
		case metrics.Histogram:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			fields := map[string]float64{
				"count":    float64(ms.Count()),
				"max":      float64(ms.Max()),
				"mean":     ms.Mean(),
				"min":      float64(ms.Min()),
				"stddev":   ms.StdDev(),
				"variance": ms.Variance(),
				"p50":      ps[0],
				"p75":      ps[1],
				"p95":      ps[2],
				"p99":      ps[3],
				"p999":     ps[4],
				"p9999":    ps[5],
			}
			for k, v := range fields {
				p := client.NewPoint(r.measurement,
					bucketTags(k, r.tags),
					map[string]interface{}{
						fmt.Sprintf("%s.histogram", name): v,
					},
					now)
				writeAPI.WritePoint(p)
			}
		case metrics.Meter:
			ms := metric.Snapshot()
			fields := map[string]float64{
				"count": float64(ms.Count()),
				"m1":    ms.Rate1(),
				"m5":    ms.Rate5(),
				"m15":   ms.Rate15(),
				"mean":  ms.RateMean(),
			}
			for k, v := range fields {
				p := client.NewPoint(r.measurement,
					bucketTags(k, r.tags),
					map[string]interface{}{
						fmt.Sprintf("%s.meter", name): v,
					},
					now)
				writeAPI.WritePoint(p)
			}

		case metrics.Timer:
			ms := metric.Snapshot()
			ps := ms.Percentiles([]float64{0.5, 0.75, 0.95, 0.99, 0.999, 0.9999})
			fields := map[string]float64{
				"count":    float64(ms.Count()),
				"max":      float64(ms.Max()),
				"mean":     ms.Mean(),
				"min":      float64(ms.Min()),
				"stddev":   ms.StdDev(),
				"variance": ms.Variance(),
				"p50":      ps[0],
				"p75":      ps[1],
				"p95":      ps[2],
				"p99":      ps[3],
				"p999":     ps[4],
				"p9999":    ps[5],
				"m1":       ms.Rate1(),
				"m5":       ms.Rate5(),
				"m15":      ms.Rate15(),
				"meanrate": ms.RateMean(),
			}
			for k, v := range fields {
				p := client.NewPoint(r.measurement,
					bucketTags(k, r.tags),
					map[string]interface{}{
						fmt.Sprintf("%s.timer", name): v,
					},
					now)
				writeAPI.WritePoint(p)
			}
		}
	})
	writeAPI.Flush()
	return nil
}

func bucketTags(bucket string, tags map[string]string) map[string]string {
	m := map[string]string{}
	for tk, tv := range tags {
		m[tk] = tv
	}
	m["bucket"] = bucket
	return m
}
