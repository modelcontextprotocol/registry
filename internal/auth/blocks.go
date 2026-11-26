package auth

// BlockedNamespaces contains a list of namespaces that are not allowed to publish packages.
// This is used as a denylist mechanism to prevent abuse.
var BlockedNamespaces = []string{
	"io.github.zz959397ghb",
}
