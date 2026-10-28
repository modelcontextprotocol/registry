package main

import (
	"testing"
	"time"
)

func TestTimelineSeparatesSchedulingQueueAndHTTPAcrossWindows(t *testing.T) {
	base := time.Unix(0, 0)
	planned := base.Add(9900 * time.Millisecond)
	queued := base.Add(10200 * time.Millisecond)
	started := base.Add(12200 * time.Millisecond)
	finished := base.Add(21200 * time.Millisecond)
	scheduling, completion := newTimeline(), newTimeline()
	scheduling.scheduled("read", planned, queued, false)
	scheduling.scheduled("read", base.Add(11*time.Second), base.Add(11*time.Second), true)
	completion.completed(point{kind: "page", status: 200, scheduledAt: planned, queuedAt: queued, startedAt: started, finishedAt: finished})
	scheduling.scheduled("publish", planned, queued, false)
	completion.completed(point{kind: "publish", status: 503, scheduledAt: planned, queuedAt: queued, startedAt: started, finishedAt: finished})
	completion.merge(scheduling)
	w := completion.windows()
	if len(w) != 3 || w[0].Reads.Scheduled != 1 || w[0].Publishes.Scheduled != 1 ||
		w[1].Reads.Scheduled != 1 || w[1].Reads.Dropped != 1 || w[1].Reads.Dispatched != 1 ||
		w[2].Reads.Completed != 1 || w[2].Publishes.Completed != 1 || w[2].Publishes.Failures != 1 {
		t.Fatalf("wrong window attribution: %+v", w)
	}
	if w[1].Reads.SchedulerLag.MeanMS != 150 || w[1].Reads.QueueDelay.MeanMS != 2000 ||
		w[1].Reads.HTTPDuration.MeanMS != 9000 || w[1].Reads.HTTPDuration.P95UpperMS != 10000 {
		t.Fatalf("timing components were conflated: %+v", w[1].Reads)
	}
	if w[0].Reads.Dispatched != 0 || w[1].Reads.Completed != 0 || w[2].Reads.Dispatched != 0 {
		t.Fatal("late completion was attributed to the wrong interval")
	}
}

func TestTimelineHistogramMergeAndOverflow(t *testing.T) {
	var a, b durationHistogram
	for range 19 {
		a.add(time.Millisecond)
	}
	b.add(3 * time.Minute)
	a.merge(b)
	s := a.summary()
	if s.Count != 20 || s.P95UpperMS != 1 || s.MaximumMS != 180000 {
		t.Fatalf("wrong percentile after merge: %+v", s)
	}
	if b.summary().P95UpperMS != 180000 || (durationHistogram{}).summary().Count != 0 {
		t.Fatal("overflow or empty histogram produced a misleading result")
	}
}

func BenchmarkTimeline(b *testing.B) {
	t := newTimeline()
	at := time.Unix(100, 0)
	p := point{kind: "page", status: 200, scheduledAt: at, queuedAt: at.Add(time.Millisecond),
		startedAt: at.Add(2 * time.Millisecond), finishedAt: at.Add(10 * time.Millisecond)}
	b.ReportAllocs()
	for b.Loop() {
		t.scheduled("read", p.scheduledAt, p.queuedAt, false)
		t.completed(p)
	}
}
