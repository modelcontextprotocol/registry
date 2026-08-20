package v0

// Shared OpenAPI operation metadata, kept in one place so the generated spec
// groups operations consistently.
const (
	// tagServers groups the server CRUD and status operations.
	tagServers = "servers"

	// securitySchemeBearer is the name of the Registry JWT bearer security
	// scheme declared in the OpenAPI spec.
	securitySchemeBearer = "bearer"
)
