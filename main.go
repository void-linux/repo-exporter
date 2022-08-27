package main

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/crc32"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Duncaen/go-xbps/repo"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "net/http/pprof"
)

const (
	namespace = "repo"
)

type RepoConfig struct {
	Url  string
	Arch string
}

type MirrorConfig struct {
	Url string
}

type Config struct {
	Repos   []RepoConfig
	Mirrors []MirrorConfig
}

type cacheHeaders struct {
	lastModified string
	etag         string
}

type Mirror struct {
	Config MirrorConfig

	cacheOriginTime        cacheHeaders
	cacheRepoSyncStartTime cacheHeaders
	cacheRepoSyncEndTime   cacheHeaders

	OriginTime    prometheus.Gauge
	SyncStartTime prometheus.Gauge
	SyncEndTime   prometheus.Gauge
}

var mirrors = map[string]*Mirror{}

type Repo struct {
	Config RepoConfig

	cacheRepoData cacheHeaders

	Checksum      prometheus.Gauge
	Staged        prometheus.Gauge
	StagePackages prometheus.Gauge
	IndexPackages prometheus.Gauge
}

type repoKey struct {
	target string
	arch   string
}

var repos = map[repoKey]*Repo{}

var client *http.Client

func fetch(url string, cache *cacheHeaders) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if cache.etag != "" {
		req.Header.Add("If-None-Match", cache.etag)
	}
	if cache.lastModified != "" {
		req.Header.Add("If-Modified-Since", cache.lastModified)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	log.Println(url, "  ", resp.Status)
	if resp.StatusCode == http.StatusNotModified {
		io.Copy(io.Discard, resp.Body)
		return nil, resp.StatusCode, err
	}
	bytes, err := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		cache.lastModified = resp.Header.Get("Last-Modified")
		cache.etag = resp.Header.Get("ETag")
	}
	return bytes, resp.StatusCode, err
}

func head(url string) (int, error) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, err
}

func (mirror *Mirror) Update() error {
	log.Println("updating mirror", mirror.Config.Url)
	g, _ := errgroup.WithContext(context.Background())

	g.Go(func() error {
		otimes, c, err := fetch(mirror.Config.Url+"/otime", &mirror.cacheOriginTime)
		if err != nil {
			return err
		}
		if c == http.StatusOK {
			// If this fails it will just stay at zero; acceptable.
			otime, err := strconv.ParseFloat(strings.TrimSpace(string(otimes)), 64)
			if err != nil {
				log.Println("Error parsing otime", err)
			}
			mirror.OriginTime.Set(otime)
		} else if c == http.StatusNotFound {
			mirror.OriginTime.Set(0)
		}
		return nil
	})
	var stimeStart float64
	g.Go(func() error {
		stimeStarts, c, err := fetch(mirror.Config.Url+"/stime-start", &mirror.cacheRepoSyncStartTime)
		if err != nil {
			return err
		}
		if c == http.StatusOK {
			// If this fails it will just stay at zero; acceptable.
			stimeStart, err = strconv.ParseFloat(strings.TrimSpace(string(stimeStarts)), 64)
			if err != nil {
				log.Println("Error parsing stimeStart", err)
			}
			mirror.SyncStartTime.Set(stimeStart)
		} else if c == http.StatusNotFound {
			mirror.SyncStartTime.Set(0)
		}
		return nil
	})
	var stimeEnd float64
	g.Go(func() error {
		stimeEnds, c, err := fetch(mirror.Config.Url+"/stime-end", &mirror.cacheRepoSyncEndTime)
		if err != nil {
			return err
		}
		if c == http.StatusOK {
			// If this fails it will just stay at zero; acceptable.
			stimeEnd, err = strconv.ParseFloat(strings.TrimSpace(string(stimeEnds)), 64)
			if err != nil {
				log.Println("Error parsing stimeEnd", err)
			}
			mirror.SyncEndTime.Set(stimeEnd)
		} else if c == http.StatusNotFound {
			mirror.SyncEndTime.Set(0)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

func (r *Repo) Update() error {
	log.Println("updating repo", r.Config.Url)
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		repodata, c, err := fetch(r.Config.Url+"/"+r.Config.Arch+"-repodata", &r.cacheRepoData)
		if err != nil {
			return err
		}
		if c == http.StatusOK {
			r.Checksum.Set(float64(crc32.ChecksumIEEE(repodata)))
			var rd *repo.Repository

			rd_reader := bytes.NewReader(repodata)
			rd = &repo.Repository{URI: nil, Arch: r.Config.Arch}
			_, err = rd.ReadFrom(rd_reader)
			if err != nil {
				log.Printf("Error reading repodata: %s", err)
			}
			r.IndexPackages.Set(float64(len(rd.Index)))
			r.StagePackages.Set(float64(len(rd.Stage)))
			if len(rd.Stage) > 0 {
				r.Staged.Set(1)
			} else {
				r.Staged.Set(0)
			}
		} else if c == http.StatusNotFound {
			r.Checksum.Set(0)
			r.IndexPackages.Set(0)
			r.StagePackages.Set(0)
			r.Staged.Set(0)
		}
		return nil
	})
	return nil
}

type Metrics struct {
	// Repository metrics
	Checksum      *prometheus.GaugeVec
	Staged        *prometheus.GaugeVec
	StagePackages *prometheus.GaugeVec
	IndexPackages *prometheus.GaugeVec

	// Mirror metrics
	OriginTime    *prometheus.GaugeVec
	SyncStartTime *prometheus.GaugeVec
	SyncEndTime   *prometheus.GaugeVec
}

func main() {
	client = &http.Client{Timeout: time.Second * 10}

	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	buf, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatal("failed to read config.json:", err)
	}

	var config Config
	if err := json.Unmarshal([]byte(buf), &config); err != nil {
		log.Fatal(err)
	}

	reg := prometheus.NewRegistry()
	metrics := Metrics{
		Checksum: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "repodata_checksum"),
				Help: "CRC32 of the repodata",
			},
			[]string{"instance", "arch"},
		),
		Staged: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "is_staged"),
				Help: "Non-zero if a stagedata file is present on the repo",
			},
			[]string{"instance", "arch"},
		),
		IndexPackages: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "packages"),
				Help: "Packages present in the repo",
			},
			[]string{"instance", "arch"},
		),
		StagePackages: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "staged_packages"),
				Help: "Staged packages present in the repo",
			},
			[]string{"instance", "arch"},
		),
		OriginTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "origin_time"),
				Help: "A Unix Timestamp updated every minute on the origin",
			},
			[]string{"instance"},
		),
		SyncStartTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "sync_start_time"),
				Help: "A Unix timestamp written by the mirror when it last started a sync",
			},
			[]string{"instance"},
		),
		SyncEndTime: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(namespace, "", "sync_end_time"),
				Help: "A Unix timestamp written by the mirror when it last finished a sync",
			},
			[]string{"instance"},
		),
	}
	reg.MustRegister(metrics.Checksum,
		metrics.Staged,
		metrics.IndexPackages,
		metrics.StagePackages,
		metrics.OriginTime,
		metrics.SyncStartTime,
		metrics.SyncEndTime,
	)

	var repos []*Repo
	for _, repoCfg := range config.Repos {
		repo := &Repo{
			Config:        repoCfg,
			Checksum:      metrics.Checksum.WithLabelValues(repoCfg.Url, repoCfg.Arch),
			Staged:        metrics.Staged.WithLabelValues(repoCfg.Url, repoCfg.Arch),
			IndexPackages: metrics.IndexPackages.WithLabelValues(repoCfg.Url, repoCfg.Arch),
			StagePackages: metrics.StagePackages.WithLabelValues(repoCfg.Url, repoCfg.Arch),
		}
		repos = append(repos, repo)
	}

	var mirrors []Mirror
	for _, mirror := range config.Mirrors {
		log.Print(mirror)
		mirrors = append(mirrors, Mirror{
			Config:        mirror,
			OriginTime:    metrics.OriginTime.WithLabelValues(mirror.Url),
			SyncStartTime: metrics.SyncStartTime.WithLabelValues(mirror.Url),
			SyncEndTime:   metrics.SyncEndTime.WithLabelValues(mirror.Url),
		})
	}

	go func() {
		for {
			for _, repo := range repos {
				go func() {
					if err := repo.Update(); err != nil {
						log.Println(err)
					}
				}()
			}
			for _, mirror := range mirrors {
				go func() {

					if err := mirror.Update(); err != nil {
						log.Println(err)
					}
				}()
			}
			time.Sleep(60 * time.Second)
		}
	}()

	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>
	<head><title>XBPS Repo Exporter</title></head>
	<body>
	<h1>XBPS Repo Exporter</h1>
	</body>
	</html>`))
	})

	http.ListenAndServe(":1234", nil)
}
