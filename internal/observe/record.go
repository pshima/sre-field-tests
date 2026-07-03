// Package observe defines the append-only result stream the observer writes and
// the scoring pipeline reads.
//
// The observer must keep working while the host it watches is degraded (CPU
// pinned, memory exhausted, file descriptors gone). The chosen format is
// therefore the most robust option available: newline-delimited JSON written
// with O_APPEND and fsync'd, one complete record per write. Append crash-safety
// is a property of that write discipline, not of JSON itself; a torn trailing
// line is discarded on read. Field names follow OpenTelemetry semantic
// conventions where one exists (e.g. system.memory.usage) so the data speaks a
// standard vocabulary even though the container is plain JSONL.
package observe

import "time"

// RecordKind distinguishes a periodic metric sample from a discrete event.
type RecordKind string

const (
	// KindSample is a periodic gauge/counter reading from a collector.
	KindSample RecordKind = "sample"
	// KindEvent is a discrete occurrence (OOM kill, container restart, exit).
	KindEvent RecordKind = "event"
)

// Record is one line in observer.jsonl. A record is either a Sample (a metric
// reading) or an Event (something happened), keyed by Kind. Keeping both in one
// stream preserves exact ordering between "memory hit the limit" and "OOM kill
// fired", which is the crux of grading a resource-exhaustion scenario.
type Record struct {
	// TS is the wall-clock time the record was produced (RFC3339 with millis).
	TS time.Time `json:"ts"`

	// Kind is "sample" or "event".
	Kind RecordKind `json:"kind"`

	// Collector is the collector ID that produced the record (e.g. "cgroup-mem",
	// "docker-events", "http-health", "proc-fd").
	Collector string `json:"collector"`

	// Target is the logical target the record concerns (a compose service name).
	Target string `json:"target,omitempty"`

	// Metric is the OTel-style metric name for samples (e.g.
	// "system.memory.usage", "http.server.request.duration"). Empty for events.
	Metric string `json:"metric,omitempty"`

	// Value is the numeric reading for samples.
	Value float64 `json:"value,omitempty"`

	// Unit is the UCUM-style unit for Value (e.g. "By" bytes, "ms", "1").
	Unit string `json:"unit,omitempty"`

	// Event is the event type for events (e.g. "oom_kill", "container_restart",
	// "container_exit"). Empty for samples.
	Event string `json:"event,omitempty"`

	// Attrs carries additional structured context (exit codes, oom_kill counts,
	// http status, fd counts). Kept as a free map so new collectors need no
	// schema change.
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Sample constructs a metric-sample record.
func Sample(ts time.Time, collector, target, metric string, value float64, unit string) Record {
	return Record{TS: ts, Kind: KindSample, Collector: collector, Target: target, Metric: metric, Value: value, Unit: unit}
}

// Event constructs a discrete-event record.
func Event(ts time.Time, collector, target, event string, attrs map[string]any) Record {
	return Record{TS: ts, Kind: KindEvent, Collector: collector, Target: target, Event: event, Attrs: attrs}
}

// Common metric names (OTel semantic conventions where applicable). Centralized
// so the observer and the grader never disagree on a string.
const (
	MetricMemoryUsage    = "system.memory.usage"           // bytes, cgroup memory.current
	MetricMemoryLimit    = "system.memory.limit"           // bytes, cgroup memory.max
	MetricOOMKillCount   = "system.memory.oom_kill.count"  // count, cgroup memory.events
	MetricProcRSS        = "process.memory.rss"            // bytes
	MetricOpenFDs        = "process.open_file_descriptors" // count
	MetricHTTPDurationMS = "http.server.request.duration"  // ms
	MetricHTTPErrorRate  = "http.server.error.rate"        // ratio 0..1
	MetricHealthUp       = "service.health.up"             // 1 healthy / 0 down
	MetricRestartCount   = "container.restart.count"       // count
)

// Common event types.
const (
	EventOOMKill          = "oom_kill"
	EventContainerRestart = "container_restart"
	EventContainerExit    = "container_exit"
	EventContainerHealth  = "container_health"
)
