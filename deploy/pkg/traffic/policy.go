// Package traffic shares the exact public-read policy with the deployment validator.
package traffic

import (
	"embed"
	"encoding/json"
)

//go:embed policy.json
var files embed.FS

type Policy struct {
	HTTPSnippet          string `json:"http_snippet"`
	LocationSnippet      string `json:"location_snippet"`
	ExclusionName        string `json:"exclusion_name"`
	ExclusionDescription string `json:"exclusion_description"`
	ExclusionFilter      string `json:"exclusion_filter"`
}

func ReadPolicy() Policy {
	data, err := files.ReadFile("policy.json")
	if err != nil {
		panic(err)
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		panic(err)
	}
	return policy
}
