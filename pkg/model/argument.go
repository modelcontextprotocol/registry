package model

import "encoding/json"

// argumentJSON avoids calling Argument's JSON methods recursively.
type argumentJSON Argument

// UnmarshalJSON preserves explicitly empty value and valueHint strings. The
// positional argument schema requires property presence, not nonempty strings.
func (a *Argument) UnmarshalJSON(data []byte) error {
	var decoded struct {
		argumentJSON
		Value     *string `json:"value"`
		ValueHint *string `json:"valueHint"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*a = Argument(decoded.argumentJSON)
	a.valuePresent = decoded.Value != nil
	a.valueHintPresent = decoded.ValueHint != nil
	if decoded.Value != nil {
		a.Value = *decoded.Value
	}
	if decoded.ValueHint != nil {
		a.ValueHint = *decoded.ValueHint
	}
	return nil
}

// MarshalJSON retains explicitly empty strings when persisting or returning an
// argument, while preserving omission for fields that were never supplied.
func (a Argument) MarshalJSON() ([]byte, error) {
	var value, valueHint *string
	if a.valuePresent || a.Value != "" {
		value = &a.Value
	}
	if a.valueHintPresent || a.ValueHint != "" {
		valueHint = &a.ValueHint
	}
	return json.Marshal(struct {
		argumentJSON
		Value     *string `json:"value,omitempty"`
		ValueHint *string `json:"valueHint,omitempty"`
	}{argumentJSON(a), value, valueHint})
}

// HasValueOrHint reports whether either positional argument property was
// supplied, including an explicitly empty string decoded from JSON.
func (a Argument) HasValueOrHint() bool {
	return a.valuePresent || a.valueHintPresent || a.Value != "" || a.ValueHint != ""
}
