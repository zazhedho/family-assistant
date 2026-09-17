package storage

import (
	"context"
	"io"
	"mime/multipart"
)

// StorageProvider defines the interface for object storage operations
type StorageProvider interface {
	// UploadFile uploads a file from multipart form and returns its storage metadata.
	UploadFile(ctx context.Context, file multipart.File, fileHeader *multipart.FileHeader, folder string) (FileInfo, error)

	// UploadFileFromBytes uploads file from byte array and returns its storage metadata.
	UploadFileFromBytes(ctx context.Context, data []byte, filename string, folder string, contentType string) (FileInfo, error)

	// DeleteFile deletes a file using its URL
	DeleteFile(ctx context.Context, fileURL string) error

	// GetFileURL returns the public URL for an object
	GetFileURL(objectName string) string

	// DownloadFile downloads a file and returns a ReadCloser
	DownloadFile(ctx context.Context, objectName string) (io.ReadCloser, error)
}

type FileInfo struct {
	ObjectName string
	URL        string
}

// Config holds the configuration for storage providers
type Config struct {
	Provider        string // "minio" or "r2"
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	UseSSL          bool
	BaseURL         string // Public URL to access files
	Region          string // For R2
	AccountID       string // For R2
}
