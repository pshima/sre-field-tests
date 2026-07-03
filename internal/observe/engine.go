package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
)

// engineClient is a minimal Docker Engine API client over the unix socket,
// built on the standard library alone. We deliberately avoid the full Docker Go
// SDK: the observer must stay a small, robust, CGO-free static binary, and the
// handful of Engine endpoints we need (inspect, one-shot stats, events) return
// stable JSON. This is the tier-0 (local Docker) source of truth for
// container-level signals — memory vs limit, OOM kills, exit codes, restarts —
// which are only observable at the Docker layer, and it works uniformly on
// Linux and Docker Desktop's VM.
type engineClient struct {
	hc *http.Client
}

// newEngineClient dials the Docker daemon at the given unix socket path.
func newEngineClient(socket string) *engineClient {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &engineClient{hc: &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", socket)
			},
			// One idle conn is plenty; the events stream uses its own request.
			MaxIdleConns:    2,
			IdleConnTimeout: 30 * time.Second,
		},
	}}
}

// containerState is the subset of GET /containers/{id}/json we consume.
type containerState struct {
	Name         string
	RestartCount int
	State        struct {
		Status     string // created|running|restarting|exited|dead|paused
		Running    bool
		OOMKilled  bool
		ExitCode   int
		StartedAt  string
		FinishedAt string
		Health     *struct {
			Status string // starting|healthy|unhealthy
		}
	}
}

// inspect returns the current state of a container by name or id.
func (e *engineClient) inspect(ctx context.Context, name string) (*containerState, error) {
	var cs containerState
	if err := e.getJSON(ctx, "/containers/"+url.PathEscape(name)+"/json", &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// memStats is the subset of GET /containers/{id}/stats we consume.
type memStats struct {
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			// cgroup v2 working-set correction; subtract inactive_file from
			// usage to approximate resident working set like `docker stats`.
			InactiveFile uint64 `json:"inactive_file"`
		} `json:"stats"`
	} `json:"memory_stats"`
}

// statsOneShot returns (workingSetBytes, limitBytes). It uses stream=false so a
// single sample returns promptly; a container that is mid-restart yields an
// error, which the caller treats as "no sample this tick".
func (e *engineClient) statsOneShot(ctx context.Context, name string) (usage, limit uint64, err error) {
	var ms memStats
	if err := e.getJSON(ctx, "/containers/"+url.PathEscape(name)+"/stats?stream=false&one-shot=true", &ms); err != nil {
		return 0, 0, err
	}
	u := ms.MemoryStats.Usage
	if ms.MemoryStats.Stats.InactiveFile < u {
		u -= ms.MemoryStats.Stats.InactiveFile
	}
	return u, ms.MemoryStats.Limit, nil
}

// engineEvent is the subset of GET /events we consume.
type engineEvent struct {
	Type   string `json:"Type"`   // "container"
	Action string `json:"Action"` // "oom","die","start","restart","health_status: healthy"
	Actor  struct {
		Attributes map[string]string `json:"Attributes"` // name, exitCode, ...
	} `json:"Actor"`
	Time int64 `json:"time"`
}

// streamEvents subscribes to container events and calls fn for each until ctx is
// done or the stream errors. It filters server-side to the given container
// names and the container type to keep the stream small.
func (e *engineClient) streamEvents(ctx context.Context, names []string, fn func(engineEvent)) error {
	filters := map[string][]string{"type": {"container"}}
	if len(names) > 0 {
		filters["container"] = names
	}
	fj, _ := json.Marshal(filters)
	q := url.Values{}
	q.Set("filters", string(fj))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/events?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("events: HTTP %d", resp.StatusCode)
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var ev engineEvent
		if err := dec.Decode(&ev); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		fn(ev)
	}
}

// ContainerStatus is a snapshot of a container's liveness, exported for the
// self-test and control plane to poll without depending on the Docker CLI.
type ContainerStatus struct {
	Status       string
	Running      bool
	OOMKilled    bool
	ExitCode     int
	RestartCount int
}

// QueryContainer returns the current status of a container by name via the
// Docker Engine unix socket.
func QueryContainer(ctx context.Context, socket, name string) (ContainerStatus, error) {
	cs, err := newEngineClient(socket).inspect(ctx, name)
	if err != nil {
		return ContainerStatus{}, err
	}
	return ContainerStatus{
		Status:       cs.State.Status,
		Running:      cs.State.Running,
		OOMKilled:    cs.State.OOMKilled,
		ExitCode:     cs.State.ExitCode,
		RestartCount: cs.RestartCount,
	}, nil
}

// getJSON performs a GET and decodes the JSON body into v.
func (e *engineClient) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := e.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
