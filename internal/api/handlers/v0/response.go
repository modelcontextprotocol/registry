package v0

// Response is a generic wrapper for Huma responses
// Usage: Response[HealthBody] instead of HealthOutput
type Response[T any] struct {
	Body T
}

// Example usage:
// Instead of:
//   type HealthOutput struct {
//       Body HealthBody
//   }
//
// You could use:
//   type HealthOutput = Response[HealthBody]
//
// Or directly in the handler:
//   func(...) (*Response[HealthBody], error) {
//       return &Response[HealthBody]{
//           Body: HealthBody{...},
//       }, nil
//   }
