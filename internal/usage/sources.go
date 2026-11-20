package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	loggingEndpoint = "https://logging.googleapis.com"
	loggingScope    = "https://www.googleapis.com/auth/logging.read"
	// A page of a thousand access records is about a megabyte.
	logPageMaxBytes = 256 << 20
	logPageSize     = 1000
	logMaxRetries   = 12
	stampLayout     = "2006-01-02T15:04:05Z"
)

// LogWindow selects the access records to read. Fraction below 1 asks Cloud
// Logging for a deterministic sample keyed on the request ID.
type LogWindow struct {
	Start    time.Time
	End      time.Time
	Fraction float64
}

// LogReader reads gateway access records. Each record is passed to the
// callback as the entry's timestamp and its decoded payload, in which numbers
// are json.Number values. An error from the callback stops the read.
type LogReader interface {
	Read(ctx context.Context, window LogWindow, each func(timestamp string, payload map[string]any) error) error
}

// MetricSample is one series of an instant query result.
type MetricSample struct {
	Labels map[string]string
	Value  float64
}

// MetricQuerier runs instant queries against the gateway's request counters.
type MetricQuerier interface {
	Query(ctx context.Context, query string, at time.Time) ([]MetricSample, error)
}

// CloudLogging reads access records from the Cloud Logging API.
type CloudLogging struct {
	Project      string
	Cluster      string
	Namespace    string
	Container    string
	QuotaProject string
	// Token returns a bearer token; it is asked again for every page so that an
	// expired token is replaced.
	Token func(ctx context.Context) (string, error)
	// Endpoint, Client, Pause and Logf are optional.
	Endpoint string
	Client   *http.Client
	Pause    time.Duration
	Logf     func(format string, args ...any)

	sleep func(ctx context.Context, wait time.Duration) error
}

type logPage struct {
	Entries []struct {
		Timestamp   string         `json:"timestamp"`
		JSONPayload map[string]any `json:"jsonPayload"`
	} `json:"entries"`
	NextPageToken string `json:"nextPageToken"`
}

var errNoToken = errors.New("could not get a Google access token")

type logAPIError struct {
	status int
	detail string
}

func (e *logAPIError) Error() string {
	if e.detail == "" {
		return fmt.Sprintf("Cloud Logging read failed with HTTP %d", e.status)
	}
	return fmt.Sprintf("Cloud Logging read failed with HTTP %d (%s)", e.status, e.detail)
}

// Filter is the Cloud Logging filter for a window. The sample clause uses the
// request ID because the storage exclusion already samples on the insert ID,
// and sampling on that again would return no listing records.
func (c *CloudLogging) Filter(window LogWindow) string {
	parts := []string{
		`resource.type="k8s_container"`,
		`resource.labels.cluster_name="` + c.Cluster + `"`,
		`resource.labels.namespace_name="` + c.Namespace + `"`,
		`resource.labels.container_name="` + c.Container + `"`,
		`jsonPayload.kind="registry_access"`,
		`timestamp>="` + window.Start.UTC().Format(stampLayout) + `"`,
		`timestamp<"` + window.End.UTC().Format(stampLayout) + `"`,
	}
	if window.Fraction < 1 {
		parts = append(parts, "sample(jsonPayload.request_id, "+strconv.FormatFloat(window.Fraction, 'f', -1, 64)+")")
	}
	return strings.Join(parts, " AND ")
}

// Read pages through the window in timestamp order.
func (c *CloudLogging) Read(ctx context.Context, window LogWindow, each func(timestamp string, payload map[string]any) error) error {
	body := map[string]any{
		"resourceNames": []string{"projects/" + c.Project},
		"filter":        c.Filter(window),
		"orderBy":       "timestamp asc",
		"pageSize":      logPageSize,
	}
	pause := c.Pause
	if pause == 0 {
		pause = 400 * time.Millisecond
	}
	pages, retries := 0, 0
	for {
		page, err := c.page(ctx, body)
		if err != nil {
			wait, retryable := retryAfter(err)
			retries++
			if !retryable || retries > logMaxRetries || ctx.Err() != nil {
				return err
			}
			c.logf("  log read interrupted (%s); retry %d", describeRetry(err), retries)
			if err := c.wait(ctx, wait); err != nil {
				return err
			}
			continue
		}
		retries = 0
		pages++
		for _, item := range page.Entries {
			if len(item.JSONPayload) == 0 {
				continue
			}
			if err := each(item.Timestamp, item.JSONPayload); err != nil {
				return err
			}
		}
		if pages%10 == 0 {
			c.logf("  read %d pages", pages)
		}
		if page.NextPageToken == "" {
			return nil
		}
		body["pageToken"] = page.NextPageToken
		if err := c.wait(ctx, pause); err != nil {
			return err
		}
	}
}

func (c *CloudLogging) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c *CloudLogging) wait(ctx context.Context, wait time.Duration) error {
	if c.sleep != nil {
		return c.sleep(ctx, wait)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryAfter says how long to wait before repeating a failed page request, and
// whether repeating it can help at all.
func retryAfter(err error) (time.Duration, bool) {
	if errors.Is(err, errNoToken) {
		return 0, false
	}
	var apiErr *logAPIError
	if !errors.As(err, &apiErr) {
		// Timeouts and connection resets: the same page is asked for again.
		return 5 * time.Second, true
	}
	switch apiErr.status {
	case http.StatusTooManyRequests:
		return 20 * time.Second, true
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
		return 0, false
	}
	return 5 * time.Second, true
}

// describeRetry names a failure without repeating anything the server sent.
func describeRetry(err error) string {
	var apiErr *logAPIError
	if errors.As(err, &apiErr) {
		return "HTTP " + strconv.Itoa(apiErr.status)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "network error"
}

func (c *CloudLogging) page(ctx context.Context, body map[string]any) (*logPage, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	token, err := c.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errNoToken, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = loggingEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v2/entries:list", bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	if c.QuotaProject != "" {
		request.Header.Set("X-Goog-User-Project", c.QuotaProject)
	}
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, logPageMaxBytes))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, &logAPIError{status: response.StatusCode, detail: apiErrorStatus(data)}
	}
	var page logPage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&page); err != nil {
		return nil, errors.New("the log API returned a page that is not valid JSON")
	}
	return &page, nil
}

// apiErrorStatus extracts the canonical status name, such as PERMISSION_DENIED,
// from an error response. The free-text message is left out.
func apiErrorStatus(data []byte) string {
	var document struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &document) != nil {
		return ""
	}
	status := document.Error.Status
	for _, r := range status {
		if (r < 'A' || r > 'Z') && r != '_' {
			return ""
		}
	}
	return status
}

// Prometheus queries a Prometheus-compatible API such as VictoriaMetrics.
type Prometheus struct {
	BaseURL string
	Client  *http.Client
	// Attempts is how many times a failed query is tried; the default is 3.
	Attempts int

	sleep func(ctx context.Context, wait time.Duration) error
}

// Query runs an instant query at the given time.
func (p *Prometheus) Query(ctx context.Context, query string, at time.Time) ([]MetricSample, error) {
	attempts := p.Attempts
	if attempts <= 0 {
		attempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			if err := p.wait(ctx, time.Duration(attempt)*2*time.Second); err != nil {
				return nil, err
			}
		}
		samples, err := p.query(ctx, query, at)
		if err == nil {
			return samples, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, lastErr
}

func (p *Prometheus) wait(ctx context.Context, wait time.Duration) error {
	if p.sleep != nil {
		return p.sleep(ctx, wait)
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *Prometheus) query(ctx context.Context, query string, at time.Time) ([]MetricSample, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Percent-encode everything: the Kubernetes API proxy, which is one way to
	// reach the metrics store, is strict about what a URL may contain.
	target := strings.TrimRight(p.BaseURL, "/") + "/api/v1/query?query=" + queryEscape(query) + "&time=" + strconv.FormatInt(at.Unix(), 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.New("the metrics URL is not valid")
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("metrics query failed: %s", describeRetry(err))
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("metrics query failed: %s", describeRetry(err))
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics query failed with HTTP %d", response.StatusCode)
	}
	var document struct {
		Data *struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &document); err != nil || document.Data == nil {
		return nil, errors.New("metrics query returned an unexpected response")
	}
	samples := make([]MetricSample, 0, len(document.Data.Result))
	for _, series := range document.Data.Result {
		if len(series.Value) != 2 {
			return nil, errors.New("metrics query returned an unexpected response")
		}
		text, _ := series.Value[1].(string)
		value, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, errors.New("metrics query returned an unexpected response")
		}
		samples = append(samples, MetricSample{Labels: series.Metric, Value: value})
	}
	return samples, nil
}

// queryEscape percent-encodes every byte outside the unreserved set.
func queryEscape(text string) string {
	const hex = "0123456789ABCDEF"
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("_.-~", c) >= 0 {
			out.WriteByte(c)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(hex[c>>4])
		out.WriteByte(hex[c&15])
	}
	return out.String()
}
