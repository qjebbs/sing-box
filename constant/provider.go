package constant

// Provider types.
const (
	ProviderHTTP       = "http"
	ProviderFile       = "file"
	ProviderCompatible = "compatible"
)

// ProviderDisplayName returns the display name of the provider type:
// HTTP, File, Compatible
func ProviderDisplayName(providerType string) string {
	switch providerType {
	case ProviderHTTP:
		return "HTTP"
	case ProviderFile:
		return "File"
	default:
		return "Compatible"
	}
}
