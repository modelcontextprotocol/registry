package main

import (
	"sort"
	"time"
)

const windowSeconds = 10

var durationBoundsMS = [...]float64{0.1, 0.5, 1, 2, 5, 10, 20, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000, 60000, 120000}

type durationSummary struct {
	Count                         int
	MeanMS, P95UpperMS, MaximumMS float64
}

type windowTraffic struct {
	Scheduled, Dropped, Dispatched, Completed, Failures int
	SchedulerLag, QueueDelay, HTTPDuration              durationSummary
}

type timeWindow struct {
	StartedAt        time.Time
	Seconds          int
	Reads, Publishes windowTraffic
}

type durationHistogram struct {
	count        int
	sum, maximum float64
	buckets      [len(durationBoundsMS) + 1]int
}

func (h *durationHistogram) add(d time.Duration) {
	ms := max(0, float64(d)/float64(time.Millisecond))
	h.count++
	h.sum += ms
	h.maximum = max(h.maximum, ms)
	h.buckets[sort.SearchFloat64s(durationBoundsMS[:], ms)]++
}

func (h *durationHistogram) merge(other durationHistogram) {
	h.count += other.count
	h.sum += other.sum
	h.maximum = max(h.maximum, other.maximum)
	for i, n := range other.buckets {
		h.buckets[i] += n
	}
}

func (h durationHistogram) summary() durationSummary {
	if h.count == 0 {
		return durationSummary{}
	}
	out := durationSummary{Count: h.count, MeanMS: h.sum / float64(h.count), MaximumMS: h.maximum}
	remaining := (95*h.count + 99) / 100
	for i, n := range h.buckets {
		remaining -= n
		if remaining <= 0 {
			out.P95UpperMS = h.maximum
			if i < len(durationBoundsMS) {
				out.P95UpperMS = durationBoundsMS[i]
			}
			break
		}
	}
	return out
}

type trafficAccumulator struct {
	scheduled, dropped, dispatched, completed, failures int
	schedulerLag, queueDelay, httpDuration              durationHistogram
}

func (a *trafficAccumulator) merge(b trafficAccumulator) {
	a.scheduled += b.scheduled
	a.dropped += b.dropped
	a.dispatched += b.dispatched
	a.completed += b.completed
	a.failures += b.failures
	a.schedulerLag.merge(b.schedulerLag)
	a.queueDelay.merge(b.queueDelay)
	a.httpDuration.merge(b.httpDuration)
}

func (a trafficAccumulator) summary() windowTraffic {
	return windowTraffic{a.scheduled, a.dropped, a.dispatched, a.completed, a.failures,
		a.schedulerLag.summary(), a.queueDelay.summary(), a.httpDuration.summary()}
}

// Each timeline has one writer. Read scheduling, publish scheduling and result
// collection merge only after all goroutines finish, avoiding per-request locks.
type timeline struct {
	buckets map[int64]*[2]trafficAccumulator
}

func newTimeline() *timeline { return &timeline{buckets: make(map[int64]*[2]trafficAccumulator)} }

func (t *timeline) at(at time.Time, kind string) *trafficAccumulator {
	key := at.Unix() / windowSeconds
	if t.buckets[key] == nil {
		t.buckets[key] = &[2]trafficAccumulator{}
	}
	index := 0
	if kind == "publish" {
		index = 1
	}
	return &t.buckets[key][index]
}

func (t *timeline) scheduled(kind string, intended, observed time.Time, dropped bool) {
	t.at(intended, kind).scheduled++
	a := t.at(observed, kind)
	a.schedulerLag.add(observed.Sub(intended))
	if dropped {
		a.dropped++
	}
}

func (t *timeline) completed(p point) {
	a := t.at(p.startedAt, p.kind)
	a.dispatched++
	a.queueDelay.add(p.startedAt.Sub(p.queuedAt))
	a.httpDuration.add(p.finishedAt.Sub(p.startedAt))
	a = t.at(p.finishedAt, p.kind)
	a.completed++
	if p.failure != "" || p.status == 0 || p.status >= 400 {
		a.failures++
	}
}

func (t *timeline) merge(other *timeline) {
	for key, groups := range other.buckets {
		if t.buckets[key] == nil {
			t.buckets[key] = &[2]trafficAccumulator{}
		}
		for i, group := range groups {
			t.buckets[key][i].merge(group)
		}
	}
}

func (t *timeline) windows() []timeWindow {
	keys := make([]int64, 0, len(t.buckets))
	for key := range t.buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]timeWindow, 0, len(keys))
	for _, key := range keys {
		groups := t.buckets[key]
		out = append(out, timeWindow{time.Unix(key*windowSeconds, 0).UTC(), windowSeconds, groups[0].summary(), groups[1].summary()})
	}
	return out
}
