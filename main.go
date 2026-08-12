package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Duncaen/go-xbps/repo"
)

const (
	namespace = "repo"
)

var (
	// Repository metrics
	Checksum      *prometheus.GaugeVec
	Staged        *prometheus.GaugeVec
	StagePackages *prometheus.GaugeVec
	IndexPackages *prometheus.GaugeVec

	// Mirror metrics
	OriginTime    *prometheus.GaugeVec
	SyncStartTime *prometheus.GaugeVec
	SyncEndTime   *prometheus.GaugeVec

	// Exporter metrics
	ScrapeErrorsTotal     *prometheus.CounterVec
	ScrapeRequestsTotal   *prometheus.CounterVec
	ScrapeRequestDuration *prometheus.HistogramVec
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	val, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(val)
	return nil
}

type Config struct {
	RepoScrapeInterval   Duration `json:"repo_scrape_interval,omitempty"`
	MirrorScrapeInterval Duration `json:"mirror_scrape_interval,omitempty"`

	Repos   map[string][]string
	Mirrors []string
}

func ReadConfig(path string) (Config, error) {
	var config Config
	buf, err := os.ReadFile(path)
	if err != nil {
		return config, fmt.Errorf("failed to open config file: %s: %w", path, err)
	}
	if err := json.Unmarshal([]byte(buf), &config); err != nil {
		return config, fmt.Errorf("failed to parse config file: %s: %w", path, err)
	}
	if config.MirrorScrapeInterval == 0 {
		config.MirrorScrapeInterval = Duration(60 * time.Second)
	}
	if config.RepoScrapeInterval == 0 {
		config.RepoScrapeInterval = Duration(60 * time.Second)
	}
	return config, nil
}

type HeaderCacheEntry struct {
	lastModified string
	etag         string
}

type HeaderCache struct {
	Entries map[string]HeaderCacheEntry
	sync.RWMutex
}

func (hc *HeaderCache) Get(key string) (HeaderCacheEntry, bool) {
	hc.RLock()
	defer hc.RUnlock()
	val, ok := hc.Entries[key]
	return val, ok
}

func (hc *HeaderCache) Set(key string, val HeaderCacheEntry) {
	hc.Lock()
	defer hc.Unlock()
	hc.Entries[key] = val
}

var (
	headerCache HeaderCache
	client      *http.Client
)

func Fetch(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}

	req.Header.Set("User-Agent", "repo-exporter/1.0")

	if entry, ok := headerCache.Get(url); ok {
		if entry.etag != "" {
			req.Header.Set("If-None-Match", entry.etag)
		}
		if entry.lastModified != "" {
			req.Header.Set("If-Modified-Since", entry.lastModified)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	slog.Info("fetched file", "url", url, "status", resp.Status)
	if resp.StatusCode == http.StatusNotModified {
		io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, err
	} else if resp.StatusCode == http.StatusOK {
		entry := HeaderCacheEntry{
			lastModified: resp.Header.Get("last-modified"),
			etag:         resp.Header.Get("etag"),
		}
		if entry.lastModified != "" || entry.etag != "" {
			headerCache.Set(url, entry)
		}
	}

	bytes, err := io.ReadAll(resp.Body)
	return bytes, resp.StatusCode, err
}

func ScrapeTimeFile(ctx context.Context, mirror, file string, metric *prometheus.GaugeVec) error {
	url := fmt.Sprintf("%s/%s", mirror, file)
	start := time.Now()
	result, status, err := Fetch(ctx, url)
	if err != nil {
		slog.Error("failed to fetch time file", "url", url, "error", err)
		ScrapeErrorsTotal.WithLabelValues(mirror).Inc()
		return nil
	}
	ScrapeRequestDuration.WithLabelValues(mirror, file).Observe(time.Since(start).Seconds())
	ScrapeRequestsTotal.WithLabelValues(mirror, file, strconv.Itoa(status)).Inc()
	if status == http.StatusOK {
		value, err := strconv.ParseFloat(strings.TrimSpace(string(result)), 64)
		if err != nil {
			metric.DeleteLabelValues(mirror)
			slog.Error("failed to parse time file", "url", url, "error", err)
		} else {
			metric.WithLabelValues(mirror).Set(value)
		}
	} else if status == http.StatusNotModified {
		// do nothing
	} else if status == http.StatusNotFound {
		metric.DeleteLabelValues(mirror)
	}
	return nil
}

func ScrapeMirror(ctx context.Context, mirror string) error {
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		return ScrapeTimeFile(ctx, mirror, "otime", OriginTime)
	})
	g.Go(func() error {
		return ScrapeTimeFile(ctx, mirror, "stime-start", SyncStartTime)
	})
	g.Go(func() error {
		return ScrapeTimeFile(ctx, mirror, "stime-end", SyncEndTime)
	})
	return g.Wait()
}

type RepoStats struct {
	Index int
	Stage int
}

func ReadRepoStats(repodata []byte, arch string) (RepoStats, error) {
	var rd *repo.Repository
	rd_reader := bytes.NewReader(repodata)
	rd = &repo.Repository{URI: nil, Arch: arch}
	_, err := rd.ReadFrom(rd_reader)
	if err != nil {
		return RepoStats{}, err
	}
	return RepoStats{
		Index: len(rd.Index),
		Stage: len(rd.Stage),
	}, nil
}

func deleteRepoLabels(url, arch string) {
	Checksum.DeleteLabelValues(url, arch)
	IndexPackages.DeleteLabelValues(url, arch)
	StagePackages.DeleteLabelValues(url, arch)
	Staged.DeleteLabelValues(url, arch)
}

func updateRepoMetrics(url, arch string, stats RepoStats, checksum float64) {
	Checksum.WithLabelValues(url, arch).Set(checksum)
	IndexPackages.WithLabelValues(url, arch).Set(float64(stats.Index))
	StagePackages.WithLabelValues(url, arch).Set(float64(stats.Stage))
	if stats.Stage > 0 {
		Staged.WithLabelValues(url, arch).Set(1)
	} else {
		Staged.WithLabelValues(url, arch).Set(0)
	}
}

func ScrapeRepo(ctx context.Context, repo, arch string) error {
	file := fmt.Sprintf("%s-repodata", arch)
	url := fmt.Sprintf("%s/%s", repo, file)
	repodata, status, err := Fetch(ctx, url)
	if err != nil {
		deleteRepoLabels(repo, arch)
		return fmt.Errorf("failed to fetch repodata: %w", err)
	}
	ScrapeRequestsTotal.WithLabelValues(repo, file, strconv.Itoa(status)).Inc()
	if status == http.StatusOK {
		stats, err := ReadRepoStats(repodata, arch)
		if err != nil {
			return fmt.Errorf("failed to read repodata: %w", err)
		}
		updateRepoMetrics(repo, arch, stats, float64(crc32.ChecksumIEEE(repodata)))
	} else if status == http.StatusNotModified {
		return nil
	} else {
		deleteRepoLabels(repo, arch)
		return fmt.Errorf("failed to get repodata: %s: http code %d", url, status)
	}
	return nil
}

func main() {
	reg := prometheus.NewRegistry()
	Checksum = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "", "repodata_checksum"),
			Help: "CRC32 of the repodata",
		},
		[]string{"instance", "arch"},
	)
	Staged = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "", "is_staged"),
			Help: "Non-zero if a stagedata file is present on the repo",
		},
		[]string{"instance", "arch"},
	)
	IndexPackages = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "", "packages"),
			Help: "Packages present in the repo",
		},
		[]string{"instance", "arch"},
	)
	StagePackages = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "", "staged_packages"),
			Help: "Staged packages present in the repo",
		},
		[]string{"instance", "arch"},
	)
	OriginTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "mirror", "origin_time"),
			Help: "A Unix Timestamp updated every minute on the origin",
		},
		[]string{"instance"},
	)
	SyncStartTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "mirror", "sync_start_time"),
			Help: "A Unix timestamp written by the mirror when it last started a sync",
		},
		[]string{"instance"},
	)
	SyncEndTime = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(namespace, "mirror", "sync_end_time"),
			Help: "A Unix timestamp written by the mirror when it last finished a sync",
		},
		[]string{"instance"},
	)
	ScrapeErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(namespace, "", "scrape_errors_total"),
			Help: "Total number of errors encountered while collecting mirror data",
		},
		[]string{"instance"},
	)
	ScrapeRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(namespace, "", "scrape_requests_total"),
			Help: "Total number of HTTP requests made by the exporter",
		},
		[]string{"instance", "file", "status"},
	)
	ScrapeRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: prometheus.BuildFQName(namespace, "", "scrape_request_duration_seconds"),
			Help: "Duration of HTTP requests made by the exporter",
		},
		[]string{"instance", "file"},
	)

	reg.MustRegister(
		Checksum,
		Staged,
		IndexPackages,
		StagePackages,
		OriginTime,
		SyncStartTime,
		SyncEndTime,
		ScrapeErrorsTotal,
		ScrapeRequestsTotal,
		ScrapeRequestDuration,
	)

	client = &http.Client{Timeout: 10 * time.Second}
	headerCache = HeaderCache{
		Entries: make(map[string]HeaderCacheEntry),
	}

	config, err := ReadConfig("config.json")
	if err != nil {
		slog.Error("failed to read config file", "error", err)
		os.Exit(1)
	}

	for arch, repos := range config.Repos {
		for _, repo := range repos {
			go func() {
				for {
					if err := ScrapeRepo(context.Background(), repo, arch); err != nil {
						slog.Error("failed to scrape repository", "repo", repo, "arch", arch, "error", err)
					}
					time.Sleep(time.Duration(config.RepoScrapeInterval))
				}
			}()
		}
	}

	for _, mirror := range config.Mirrors {
		go func() {
			for {
				if err := ScrapeMirror(context.Background(), mirror); err != nil {
					slog.Error("failed to scrape mirror", "mirror", mirror, "error", err)
				}
				time.Sleep(time.Duration(config.MirrorScrapeInterval))
			}
		}()
	}

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html>
<html>
<head><title>XBPS Repo Exporter</title></head>
<body>
<h1>XBPS Repo Exporter</h1>
<ul>
<li><a href="/metrics">metrics</a></li>
</ul>
</body>
</html>`))
	})
	http.ListenAndServe(":1234", nil)
}
