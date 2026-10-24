package model_test

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestArgumentJSONRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  string
		hasAny bool
	}{
		{"missing", `{"type":"positional"}`, false},
		{"empty value", `{"type":"positional","value":""}`, true},
		{"empty hint", `{"type":"positional","valueHint":""}`, true},
		{"both empty", `{"type":"positional","value":"","valueHint":""}`, true},
		{"value", `{"type":"positional","value":"input.txt"}`, true},
		{"hint", `{"type":"positional","valueHint":"file"}`, true},
		{"all fields", `{"type":"named","name":"--file","value":"{file}","valueHint":"file","description":"Input file","isRequired":true,"format":"filepath","isSecret":true,"default":"default.txt","placeholder":"example.txt","choices":["a","b"],"isRepeated":true,"variables":{"file":{"value":"example.txt","isSecret":true}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var arg model.Argument
			require.NoError(t, json.Unmarshal([]byte(tc.input), &arg))
			require.Equal(t, tc.hasAny, arg.HasValueOrHint())
			encoded, err := json.Marshal(arg)
			require.NoError(t, err)
			require.JSONEq(t, tc.input, string(encoded))
			var stored model.Argument
			require.NoError(t, json.Unmarshal(encoded, &stored))
			require.Equal(t, tc.hasAny, stored.HasValueOrHint())
		})
	}
}

func TestArgumentJSONMissingAndInvalidValues(t *testing.T) {
	var arg model.Argument
	for _, input := range []string{
		`{"type":"positional","value":1}`,
		`{"type":"positional","valueHint":false}`,
	} {
		require.Error(t, json.Unmarshal([]byte(input), &arg))
	}
	for _, input := range []string{
		`{"type":"positional","value":null}`,
		`{"type":"positional","valueHint":null}`,
	} {
		require.NoError(t, json.Unmarshal([]byte(input), &arg))
		require.False(t, arg.HasValueOrHint())
	}
}

func TestArgumentJSONFromGoValue(t *testing.T) {
	arg := model.Argument{
		Type:               model.ArgumentTypePositional,
		InputWithVariables: model.InputWithVariables{Input: model.Input{Value: "file.txt"}},
	}
	encoded, err := json.Marshal(arg)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"positional","value":"file.txt"}`, string(encoded))
	require.True(t, arg.HasValueOrHint())
}
