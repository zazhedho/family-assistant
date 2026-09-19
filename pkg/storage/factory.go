package storage

import (
	"fmt"
	"github.com/zazhedho/family-assistant/utils"
)

// NewStorageProvider creates a new storage provider based on the configuration
func NewStorageProvider(config Config) (StorageProvider, error) {
	provider := utils.NormalizeKey(config.Provider)

	switch provider {
	case "minio":
		return NewMinIOAdapter(config)
	case "r2", "cloudflare", "cloudflare-r2":
		return NewR2Adapter(config)
	default:
		return nil, fmt.Errorf("unsupported storage provider: %s (supported: minio, r2)", config.Provider)
	}
}
