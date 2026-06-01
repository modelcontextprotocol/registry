package registries

// ValidateCargoREADME exposes the package-private validateCargoREADME to the
// external _test package so httptest-driven tests can exercise the README-fetch
// and mcp-name token-match pipeline against a mock server, bypassing the
// exact-baseURL guard that the public ValidateCargo enforces.
//
// Intended for cargo_test.go's positive-path and transient-error tests only.
var ValidateCargoREADME = validateCargoREADME
