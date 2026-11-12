package commands

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const (
	publishRetryInitialDelay = 5 * time.Second
	publishRetryMaxDelay     = 30 * time.Second

	// registryValidationMarker prefixes every package failure the registry reports,
	// retryable or not (validateRegistryOwnership in internal/validators/validators.go).
	registryValidationMarker = "registry validation failed for package"
)

// notVisibleYetPhrases are the lower-cased validator wordings that mean the package
// may simply not have reached its registry yet, or that its registry could not be
// asked. Everything else under registryValidationMarker needs a change from the
// publisher, so it fails on the first attempt. Wordings live in
// internal/validators/registries; TestWaitClassifierMatchesRealValidatorOutput
// runs those validators and fails if the two drift apart.
var notVisibleYetPhrases = []string{
	"not found (status: 404)",                // npm, PyPI: package or version absent
	"likely transient",                       // npm, PyPI, cargo: upstream 429/5xx or inconclusive probe
	"not found on crates.io",                 // cargo: version absent
	"rendered readme could not be retrieved", // cargo: crates.io renders the README after publish
	"does not exist in the registry",         // NuGet, OCI: package, version or image absent
	"retry shortly",                          // OCI: upstream rate limit
	"not publicly accessible (status: 404)",  // MCPB: release asset absent
}

// Sleep and clock for the --wait retry loop; swapped out in tests.
var (
	publishRetrySleep = time.Sleep
	publishRetryNow   = time.Now
)

// publishRequestGrace is how far past the --wait deadline a request may run, so an
// attempt started near the deadline can finish. It matches the slowest validator
// (the 30s OCI timeout) and bounds how long a hung request can overrun --wait.
var publishRequestGrace = 30 * time.Second

// publishError is returned by publishToRegistry for non-2xx responses so the
// caller can inspect the status and body without parsing the message.
type publishError struct {
	StatusCode int
	Body       []byte
}

func (e *publishError) Error() string {
	return fmt.Sprintf("server returned status %d: %s", e.StatusCode, e.Body)
}

// notVisibleYetMessage returns the server's message when err is a 400 reporting a
// package that is not visible yet, which is the only failure --wait retries.
func notVisibleYetMessage(err error) (string, bool) {
	var pubErr *publishError
	if !errors.As(err, &pubErr) || pubErr.StatusCode != http.StatusBadRequest {
		return "", false
	}

	var body struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(pubErr.Body, &body) != nil {
		return "", false
	}

	for _, e := range body.Errors {
		if !strings.Contains(e.Message, registryValidationMarker) {
			continue
		}
		lower := strings.ToLower(e.Message)
		for _, phrase := range notVisibleYetPhrases {
			if strings.Contains(lower, phrase) {
				return e.Message, true
			}
		}
	}
	return "", false
}

// tokenExpiry reads the exp claim from a JWT payload without checking the
// signature. It reports false for anything that is not a JWT carrying exp.
func tokenExpiry(token string) (time.Time, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(int64(claims.Exp), 0), true
}

// warnIfTokenExpiresDuringWait warns when the --wait deadline runs past the token.
// The publisher reads the token once, so retries after its expiry get a 401.
func warnIfTokenExpiresDuringWait(token string, wait time.Duration) {
	expiry, ok := tokenExpiry(token)
	if !ok || !publishRetryNow().Add(wait).After(expiry) {
		return
	}
	_, _ = fmt.Fprintf(os.Stdout, "Warning: the registry token expires at %s, before --wait %s ends. Retries after that fail with 401. Log in right before publishing or use a shorter --wait.\n",
		expiry.UTC().Format(time.RFC3339), wait)
}

// waitAbortError explains why a --wait loop stopped on a failure it does not retry,
// keeping the validation failure that started the wait when there was one.
func waitAbortError(err error, statusCode int, lastValidationErr error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded) && lastValidationErr != nil:
		return fmt.Errorf("--wait deadline passed during a request, last validation failure: %w", lastValidationErr)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("--wait deadline passed during a request: %w", err)
	case statusCode == http.StatusUnauthorized && lastValidationErr != nil:
		return fmt.Errorf("registry token expired during --wait, last validation failure: %w", lastValidationErr)
	default:
		return err
	}
}

// publishWithRetry calls publishToRegistry, retrying "not visible yet" failures with
// exponential backoff until wait has elapsed. A zero wait means one attempt with no
// deadline; otherwise each request must finish by the deadline plus publishRequestGrace.
func publishWithRetry(registryURL string, serverData []byte, token string, wait time.Duration) (*apiv0.ServerResponse, int, error) {
	if wait == 0 {
		return publishToRegistry(context.Background(), registryURL, serverData, token)
	}

	warnIfTokenExpiresDuringWait(token, wait)

	deadline := publishRetryNow().Add(wait)
	delay := publishRetryInitialDelay
	var lastValidationErr error

	for attempt := 1; ; attempt++ {
		timeout := max(deadline.Sub(publishRetryNow()), 0) + publishRequestGrace
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		response, statusCode, err := publishToRegistry(ctx, registryURL, serverData, token)
		cancel()
		if err == nil {
			return response, statusCode, nil
		}

		message, retryable := notVisibleYetMessage(err)
		if !retryable {
			return response, statusCode, waitAbortError(err, statusCode, lastValidationErr)
		}
		lastValidationErr = err

		remaining := deadline.Sub(publishRetryNow())
		if remaining <= 0 {
			return response, statusCode, err
		}
		if delay > remaining {
			delay = remaining
		}

		_, _ = fmt.Fprintf(os.Stdout, "Not visible yet (attempt %d): %s\nRetrying in %s...\n", attempt, message, delay)
		publishRetrySleep(delay)

		delay *= 2
		if delay > publishRetryMaxDelay {
			delay = publishRetryMaxDelay
		}
	}
}

// parsePublishArgs returns the server.json path and the --wait duration.
func parsePublishArgs(args []string) (string, time.Duration, error) {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	waitStr := fs.String("wait", "", "Retry while the package is not visible yet, for up to this duration (e.g. 4m)")

	if err := fs.Parse(args); err != nil {
		return "", 0, err
	}

	var wait time.Duration
	if *waitStr != "" {
		parsed, err := time.ParseDuration(*waitStr)
		if err != nil || parsed < 0 {
			return "", 0, fmt.Errorf("invalid --wait duration %q: expected a duration such as 30s or 4m", *waitStr)
		}
		wait = parsed
	}

	if fs.NArg() > 1 {
		return "", 0, fmt.Errorf("flags must come before the path: expected 'mcp-publisher publish [flags] [server.json]', got %q", fs.Args())
	}

	serverFile := "server.json"
	if fs.NArg() > 0 {
		serverFile = fs.Arg(0)
	}
	return serverFile, wait, nil
}

func PublishCommand(args []string) error {
	serverFile, wait, err := parsePublishArgs(args)
	if err != nil {
		return err
	}

	// Read server.json
	serverData, err := os.ReadFile(serverFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("server.json not found. Run 'mcp-publisher init' to create one")
		}
		return fmt.Errorf("failed to read server.json: %w", err)
	}
	if err := validateJSONUnicode(serverFile, serverData); err != nil {
		return err
	}

	// Validate JSON
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverData, &serverJSON); err != nil {
		return fmt.Errorf("invalid server.json: %w", err)
	}

	// Load saved token
	tokenPath, err := tokenFilePath()
	if err != nil {
		return err
	}

	tokenData, err := os.ReadFile(tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return notAuthenticatedError()
		}
		return fmt.Errorf("failed to read token: %w", err)
	}

	var tokenInfo map[string]string
	if err := json.Unmarshal(tokenData, &tokenInfo); err != nil {
		return fmt.Errorf("invalid token data: %w", err)
	}

	token := tokenInfo["token"]
	registryURL := tokenInfo["registry"]
	if registryURL == "" {
		registryURL = DefaultRegistryURL
	}

	// Publish to registry
	_, _ = fmt.Fprintf(os.Stdout, "Publishing %s@%s to %s...\n", serverJSON.Name, serverJSON.Version, registryURL)
	response, statusCode, err := publishWithRetry(registryURL, serverData, token, wait)
	if err != nil {
		// If publish failed with 422, call validate endpoint to show detailed errors
		if statusCode == http.StatusUnprocessableEntity {
			_, _ = fmt.Fprintln(os.Stdout, "Validation failed. Checking detailed validation errors...")
			_, _ = fmt.Fprintln(os.Stdout)

			// Call validate endpoint (same as validate command does)
			result, validateErr := validateViaAPI(registryURL, serverData)
			if validateErr != nil {
				// If validate also fails, return original publish error
				return fmt.Errorf("publish failed: %w", err)
			}

			// Print validation results using shared formatting logic
			formattedErrorMsg := printValidationIssues(result, &serverJSON)

			if !result.Valid {
				// Return error with formatted message if available
				if formattedErrorMsg != "" {
					return fmt.Errorf("%s", formattedErrorMsg)
				}
				return fmt.Errorf("validation failed")
			}
		}

		// For non-422 errors, return the original error
		return fmt.Errorf("publish failed: %w", err)
	}

	_, _ = fmt.Fprintln(os.Stdout, "✓ Successfully published")
	_, _ = fmt.Fprintf(os.Stdout, "✓ Server %s version %s\n", response.Server.Name, response.Server.Version)

	return nil
}

func publishToRegistry(ctx context.Context, registryURL string, serverData []byte, token string) (*apiv0.ServerResponse, int, error) {
	if err := validateJSONUnicode("server.json", serverData); err != nil {
		return nil, 0, err
	}

	// Parse the server JSON data
	var serverJSON apiv0.ServerJSON
	err := json.Unmarshal(serverData, &serverJSON)
	if err != nil {
		return nil, 0, fmt.Errorf("error parsing server.json file: %w", err)
	}

	// Convert to JSON
	jsonData, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, 0, fmt.Errorf("error serializing request: %w", err)
	}

	// Ensure URL ends with the publish endpoint
	if !strings.HasSuffix(registryURL, "/") {
		registryURL += "/"
	}
	publishURL := registryURL + "v0/publish"

	// Create and send request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, publishURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, 0, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("error sending request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("error reading response: %w", err)
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, &publishError{StatusCode: resp.StatusCode, Body: body}
	}

	var serverResponse apiv0.ServerResponse
	if err := json.Unmarshal(body, &serverResponse); err != nil {
		return nil, resp.StatusCode, err
	}

	return &serverResponse, resp.StatusCode, nil
}
