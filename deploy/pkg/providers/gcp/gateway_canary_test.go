package gcp //nolint:testpackage // Check the private exclusion filter.

import (
	"regexp"
	"testing"
)

// Cloud Logging evaluates these patterns with RE2 and does not unescape
// backslashes first, so Go's regexp sees them exactly as the service does.
func TestGatewayAccessLogExclusionRegexes(t *testing.T) {
	for _, tt := range []struct {
		field    string
		sampled  []string
		retained []string
	}{
		{
			field:    "uri",
			sampled:  []string{"/v0/servers", "/v0.1/servers", "/v0.1/servers?cursor=a%2Fb:1.0.0&limit=100"},
			retained: []string{"/v0.1/servers/io.example%2Fa/versions/latest", "/v0x1/servers", "/v0.1/serversX", "/v0.1/publish"},
		},
		{
			field:    "duration",
			sampled:  []string{"0.000", "0.999"},
			retained: []string{"1.000", "10.250"},
		},
	} {
		t.Run(tt.field, func(t *testing.T) {
			clause := regexp.MustCompile(`(?m)^jsonPayload\.` + tt.field + ` =~ "([^"]+)"$`).FindStringSubmatch(gatewayAccessLogExclusionFilter)
			if clause == nil {
				t.Fatalf("no %s regex clause in the exclusion filter", tt.field)
			}
			pattern, err := regexp.Compile(clause[1])
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range tt.sampled {
				if !pattern.MatchString(value) {
					t.Errorf("%s %q must be eligible for sampling by %s", tt.field, value, clause[1])
				}
			}
			for _, value := range tt.retained {
				if pattern.MatchString(value) {
					t.Errorf("%s %q must always be retained, but matches %s", tt.field, value, clause[1])
				}
			}
		})
	}
}
