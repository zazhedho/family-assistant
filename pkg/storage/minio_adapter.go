package storage

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOAdapter implements StorageProvider for MinIO
type MinIOAdapter struct {
	client     *minio.Client
	bucketName string
	baseURL    string
}

// NewMinIOAdapter creates a new MinIO storage adapter
func NewMinIOAdapter(config Config) (StorageProvider, error) {
	minioClient, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKeyID, config.SecretAccessKey, ""),
		Secure: config.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	ctx := context.Background()
	exists, err := minioClient.BucketExists(ctx, config.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		err = minioClient.MakeBucket(ctx, config.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}

		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": {"AWS": ["*"]},
				"Action": ["s3:GetObject"],
				"Resource": ["arn:aws:s3:::%s/*"]
			}]
		}`, config.BucketName)

		err = minioClient.SetBucketPolicy(ctx, config.BucketName, policy)
		if err != nil {
			return nil, fmt.Errorf("failed to set bucket policy: %w", err)
		}
	}

	return &MinIOAdapter{
		client:     minioClient,
		bucketName: config.BucketName,
		baseURL:    config.BaseURL,
	}, nil
}

func (m *MinIOAdapter) UploadFile(ctx context.Context, file multipart.File, fileHeader *multipart.FileHeader, folder string) (FileInfo, error) {
	objectName := buildObjectName(fileHeader.Filename, folder)

	fileSize := fileHeader.Size

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := m.client.PutObject(ctx, m.bucketName, objectName, file, fileSize, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to upload file: %w", err)
	}

	return FileInfo{ObjectName: objectName, URL: m.GetFileURL(objectName)}, nil
}

func (m *MinIOAdapter) UploadFileFromBytes(ctx context.Context, data []byte, filename string, folder string, contentType string) (FileInfo, error) {
	objectName := buildObjectName(filename, folder)

	if contentType == "" {
		contentType = "application/octet-stream"
	}

	reader := strings.NewReader(string(data))
	_, err := m.client.PutObject(ctx, m.bucketName, objectName, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return FileInfo{}, fmt.Errorf("failed to upload file: %w", err)
	}

	return FileInfo{ObjectName: objectName, URL: m.GetFileURL(objectName)}, nil
}

func (m *MinIOAdapter) DeleteFile(ctx context.Context, fileURL string) error {
	objectName := m.extractObjectName(fileURL)
	if objectName == "" {
		return fmt.Errorf("invalid file URL")
	}

	err := m.client.RemoveObject(ctx, m.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

func (m *MinIOAdapter) GetFileURL(objectName string) string {
	return fmt.Sprintf("%s/%s/%s", m.baseURL, m.bucketName, objectName)
}

func (m *MinIOAdapter) DownloadFile(ctx context.Context, objectName string) (io.ReadCloser, error) {
	object, err := m.client.GetObject(ctx, m.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to download file: %w", err)
	}
	return object, nil
}

func (m *MinIOAdapter) extractObjectName(fileURL string) string {
	parts := strings.Split(fileURL, "/")
	if len(parts) < 2 {
		return ""
	}

	bucketIndex := -1
	for i, part := range parts {
		if part == m.bucketName {
			bucketIndex = i
			break
		}
	}

	if bucketIndex == -1 || bucketIndex >= len(parts)-1 {
		return ""
	}

	return strings.Join(parts[bucketIndex+1:], "/")
}
