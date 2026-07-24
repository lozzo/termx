package managed

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/muxvia/muxvia/proto/cloudpb"
)

const (
	maxEnrollmentProbeCandidates = 100
	maxEnrollmentProbeWorkers    = 16
	enrollmentProbeTimeout       = 3 * time.Second
)

// ProbeEnrollmentCandidates 对 Controller 返回的有界 Hub 候选执行并发 health 延迟探测。
//
// 本函数只产生可达性观测，不缓存 Hub 目录。调用方通过 PreferredEnrollmentHub 做本地提议，
// Controller 仍拥有容量、健康、套餐、assignment epoch 和 token audience 真值。公网候选必须使用 HTTPS；loopback
// HTTP 只用于仓库内 development harness。
func ProbeEnrollmentCandidates(ctx context.Context, candidates []*cloudpb.HubEnrollmentCandidate) ([]*cloudpb.HubReachabilityObservation, error) {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return probeEnrollmentCandidates(ctx, client, candidates, maxEnrollmentProbeWorkers, enrollmentProbeTimeout)
}

// PreferredEnrollmentHub 从本机实测结果选择最低延迟的可达 Hub。
// 返回值只是 daemon 提议，Controller 仍必须校验候选摘要、容量和已有 assignment。
func PreferredEnrollmentHub(observations []*cloudpb.HubReachabilityObservation) (string, error) {
	ranked, err := RankedEnrollmentHubs(observations)
	if err != nil {
		return "", err
	}
	return ranked[0], nil
}

// RankedEnrollmentHubs 按可达性、RTT 和稳定 Hub ID 返回 daemon 的本地候选顺序。
// 该顺序只用于同一已批准 flow 的首选提议和 stale 重试，不创建 assignment 真值。
func RankedEnrollmentHubs(observations []*cloudpb.HubReachabilityObservation) ([]string, error) {
	reachable := make([]*cloudpb.HubReachabilityObservation, 0, len(observations))
	seen := make(map[string]bool, len(observations))
	for _, observation := range observations {
		if observation == nil || observation.GetHubId() == "" || seen[observation.GetHubId()] || observation.GetReachable() != (observation.GetLatencyMillis() > 0) {
			return nil, fmt.Errorf("invalid Hub enrollment observation")
		}
		seen[observation.GetHubId()] = true
		if !observation.GetReachable() {
			continue
		}
		reachable = append(reachable, observation)
	}
	if len(reachable) == 0 {
		return nil, fmt.Errorf("no reachable Hub enrollment candidate")
	}
	sort.Slice(reachable, func(left, right int) bool {
		if reachable[left].GetLatencyMillis() != reachable[right].GetLatencyMillis() {
			return reachable[left].GetLatencyMillis() < reachable[right].GetLatencyMillis()
		}
		return reachable[left].GetHubId() < reachable[right].GetHubId()
	})
	result := make([]string, len(reachable))
	for index, observation := range reachable {
		result[index] = observation.GetHubId()
	}
	return result, nil
}

func probeEnrollmentCandidates(ctx context.Context, client *http.Client, candidates []*cloudpb.HubEnrollmentCandidate, workers int, timeout time.Duration) ([]*cloudpb.HubReachabilityObservation, error) {
	if ctx == nil || client == nil || workers <= 0 || workers > maxEnrollmentProbeWorkers || timeout <= 0 || len(candidates) == 0 || len(candidates) > maxEnrollmentProbeCandidates {
		return nil, fmt.Errorf("invalid Hub enrollment probe configuration")
	}
	type job struct {
		index     int
		candidate *cloudpb.HubEnrollmentCandidate
	}
	jobs := make(chan job)
	results := make([]*cloudpb.HubReachabilityObservation, len(candidates))
	errCh := make(chan error, 1)
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if err := validateEnrollmentProbeCandidate(candidate); err != nil {
			return nil, err
		}
		if seen[candidate.GetHubId()] {
			return nil, fmt.Errorf("duplicate Hub enrollment candidate %q", candidate.GetHubId())
		}
		seen[candidate.GetHubId()] = true
	}
	if workers > len(candidates) {
		workers = len(candidates)
	}
	workerDone := make(chan struct{}, workers)
	for range workers {
		go func() {
			defer func() { workerDone <- struct{}{} }()
			for current := range jobs {
				observation, err := probeEnrollmentCandidate(ctx, client, current.candidate, timeout)
				if err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				results[current.index] = observation
			}
		}()
	}
	for index, candidate := range candidates {
		select {
		case jobs <- job{index: index, candidate: candidate}:
		case err := <-errCh:
			close(jobs)
			for range workers {
				<-workerDone
			}
			return nil, err
		case <-ctx.Done():
			close(jobs)
			for range workers {
				<-workerDone
			}
			return nil, ctx.Err()
		}
	}
	close(jobs)
	for range workers {
		select {
		case <-workerDone:
		case err := <-errCh:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	select {
	case err := <-errCh:
		return nil, err
	default:
	}
	return results, nil
}

func validateEnrollmentProbeCandidate(candidate *cloudpb.HubEnrollmentCandidate) error {
	if candidate == nil || strings.TrimSpace(candidate.GetHubId()) == "" || candidate.GetHubUrl() == "" || candidate.GetRegion() == "" {
		return fmt.Errorf("invalid Hub enrollment candidate")
	}
	parsed, err := url.Parse(candidate.GetHealthUrl())
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid Hub enrollment health URL")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	host := parsed.Hostname()
	if parsed.Scheme != "http" || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("Hub enrollment health URL must use HTTPS")
	}
	return nil
}

func probeEnrollmentCandidate(ctx context.Context, client *http.Client, candidate *cloudpb.HubEnrollmentCandidate, timeout time.Duration) (*cloudpb.HubReachabilityObservation, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, candidate.GetHealthUrl(), nil)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	response, err := client.Do(request)
	latency := time.Since(started)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &cloudpb.HubReachabilityObservation{HubId: candidate.GetHubId()}, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	_ = response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return &cloudpb.HubReachabilityObservation{HubId: candidate.GetHubId()}, nil
	}
	millis := uint64(latency.Milliseconds())
	if millis == 0 {
		millis = 1
	}
	return &cloudpb.HubReachabilityObservation{HubId: candidate.GetHubId(), Reachable: true, LatencyMillis: millis}, nil
}
