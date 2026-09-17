package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

func buildObjectName(filename, folder string) string {
	ext := filepath.Ext(filename)
	uniqueFilename := fmt.Sprintf("%s_%s%s", time.Now().Format("20060102_150405"), uuid.New().String()[:8], ext)
	if folder == "" {
		return uniqueFilename
	}

	return fmt.Sprintf("%s/%s", strings.Trim(folder, "/"), uniqueFilename)
}
