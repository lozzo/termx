package discovery

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type HubProbeResult struct {
	URL       string
	Latency   time.Duration
	Available bool
}

func ProbeHub(ctx context.Context, hubURL string, timeout time.Duration) HubProbeResult {
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, http.MethodHead, strings.TrimRight(hubURL, "/")+"/api/health", nil)
	if err != nil {
		return HubProbeResult{URL: hubURL, Latency: -1}
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return HubProbeResult{URL: hubURL, Latency: -1}
	}
	_ = resp.Body.Close()
	return HubProbeResult{URL: hubURL, Latency: elapsed, Available: resp.StatusCode >= 200 && resp.StatusCode < 500}
}

// ProbeHubs concurrently probes each hub and returns results sorted by latency.
func ProbeHubs(ctx context.Context, urls []string, timeout time.Duration, probeCount int) []HubProbeResult {
	if probeCount <= 0 {
		probeCount = 1
	}
	results := make([]HubProbeResult, len(urls))
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			var lats []time.Duration
			for j := 0; j < probeCount; j++ {
				r := ProbeHub(ctx, url, timeout)
				if r.Available {
					lats = append(lats, r.Latency)
				}
			}
			if len(lats) == 0 {
				results[i] = HubProbeResult{URL: url, Latency: -1}
				return
			}
			sort.Slice(lats, func(a, b int) bool { return lats[a] < lats[b] })
			results[i] = HubProbeResult{URL: url, Latency: lats[len(lats)/2], Available: true}
		}(i, url)
	}
	wg.Wait()
	sort.SliceStable(results, func(i, j int) bool {
		if !results[i].Available {
			return false
		}
		if !results[j].Available {
			return true
		}
		return results[i].Latency < results[j].Latency
	})
	return results
}
