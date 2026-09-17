package storage

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type storageTestMultipartFile struct {
	*bytes.Reader
}

func (f storageTestMultipartFile) Close() error {
	return nil
}

func TestNewStorageProviderRejectsUnknownProvider(t *testing.T) {
	_, err := NewStorageProvider(Config{Provider: "local"})
	if err == nil || !strings.Contains(err.Error(), "unsupported storage provider") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}

func TestNewStorageProviderCreatesR2AdapterAliases(t *testing.T) {
	provider, err := NewStorageProvider(Config{
		Provider:        "cloudflare-r2",
		Endpoint:        "https://example.r2.cloudflarestorage.com",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		BucketName:      "bucket",
		BaseURL:         "https://cdn.example.com",
		UseSSL:          true,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if _, ok := provider.(*R2Adapter); !ok {
		t.Fatalf("expected R2 adapter, got %T", provider)
	}
}

func TestNewMinIOAdapterRejectsInvalidEndpoint(t *testing.T) {
	_, err := NewMinIOAdapter(Config{
		Endpoint:        "://bad-endpoint",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		BucketName:      "bucket",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to create MinIO client") {
		t.Fatalf("expected invalid endpoint error, got %v", err)
	}
}

func TestNewStorageProviderRejectsInvalidMinIOEndpoint(t *testing.T) {
	_, err := NewStorageProvider(Config{
		Provider:        "minio",
		Endpoint:        "://bad-endpoint",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		BucketName:      "bucket",
	})
	if err == nil || !strings.Contains(err.Error(), "failed to create MinIO client") {
		t.Fatalf("expected invalid minio endpoint error, got %v", err)
	}
}

func TestNewStorageProviderCreatesR2AdapterWithDefaults(t *testing.T) {
	provider, err := NewStorageProvider(Config{
		Provider:        "r2",
		Endpoint:        "custom-endpoint",
		AccountID:       "account-1",
		AccessKeyID:     "access",
		SecretAccessKey: "secret",
		BucketName:      "bucket",
		UseSSL:          true,
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	r2, ok := provider.(*R2Adapter)
	if !ok {
		t.Fatalf("expected R2 adapter, got %T", provider)
	}
	if r2.baseURL != "https://bucket.r2.dev" || r2.accountID != "account-1" {
		t.Fatalf("unexpected r2 defaults: %+v", r2)
	}
}

func TestBuildObjectNameKeepsExtensionAndTrimsFolder(t *testing.T) {
	got := buildObjectName("avatar.png", "/users/")
	if !strings.HasPrefix(got, "users/") {
		t.Fatalf("expected folder prefix, got %q", got)
	}
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("expected extension to be preserved, got %q", got)
	}

	got = buildObjectName("report", "")
	if strings.Contains(got, "/") || strings.HasSuffix(got, ".") {
		t.Fatalf("expected bare generated object name without folder or extension, got %q", got)
	}
}

func TestExtractObjectNameFromProviderURLs(t *testing.T) {
	minio := &MinIOAdapter{bucketName: "uploads", baseURL: "http://localhost:9000"}
	if got := minio.extractObjectName("http://localhost:9000/uploads/users/avatar.png"); got != "users/avatar.png" {
		t.Fatalf("unexpected minio object name: %q", got)
	}
	if got := minio.GetFileURL("users/avatar.png"); got != "http://localhost:9000/uploads/users/avatar.png" {
		t.Fatalf("unexpected minio file url: %q", got)
	}
	if got := minio.extractObjectName("avatar.png"); got != "" {
		t.Fatalf("expected empty object name for short url, got %q", got)
	}
	if got := minio.extractObjectName("http://localhost:9000/uploads"); got != "" {
		t.Fatalf("expected empty object name for bucket-only url, got %q", got)
	}
	if err := minio.DeleteFile(context.Background(), "http://localhost:9000/no-bucket/avatar.png"); err == nil {
		t.Fatal("expected invalid minio url error")
	}

	r2 := &R2Adapter{baseURL: "https://cdn.example.com"}
	if got := r2.extractObjectName("https://cdn.example.com/users/avatar.png"); got != "users/avatar.png" {
		t.Fatalf("unexpected r2 object name: %q", got)
	}
	if got := r2.extractObjectName("https://pub.example.com/users/avatar.png"); got != "users/avatar.png" {
		t.Fatalf("unexpected r2 fallback object name: %q", got)
	}
	if got := r2.extractObjectName("avatar.png"); got != "avatar.png" {
		t.Fatalf("expected bare r2 object name, got %q", got)
	}
	if got := r2.GetFileURL("users/avatar.png"); got != "https://cdn.example.com/users/avatar.png" {
		t.Fatalf("unexpected r2 file url: %q", got)
	}
	if err := r2.DeleteFile(context.Background(), ""); err == nil {
		t.Fatal("expected invalid r2 url error")
	}
}

func TestStorageAdaptersReturnErrorsWithCanceledContext(t *testing.T) {
	client, err := minio.New("localhost:9000", &minio.Options{
		Creds:  credentials.NewStaticV4("access", "secret", ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("create minio client: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	minioAdapter := &MinIOAdapter{client: client, bucketName: "bucket", baseURL: "http://localhost:9000"}
	minioHeader := &multipart.FileHeader{
		Filename: "hello.txt",
		Size:     int64(len("hello")),
		Header:   textproto.MIMEHeader{},
	}
	if _, err := minioAdapter.UploadFile(ctx, storageTestMultipartFile{bytes.NewReader([]byte("hello"))}, minioHeader, "docs"); err == nil {
		t.Fatal("expected minio multipart upload error")
	}
	if _, err := minioAdapter.UploadFileFromBytes(ctx, []byte("hello"), "hello.txt", "docs", ""); err == nil {
		t.Fatal("expected minio upload error")
	}
	if err := minioAdapter.DeleteFile(ctx, "http://localhost:9000/bucket/docs/hello.txt"); err == nil {
		t.Fatal("expected minio delete error")
	}
	minioObject, err := minioAdapter.DownloadFile(ctx, "docs/hello.txt")
	if err != nil {
		t.Fatalf("expected minio download object creation, got %v", err)
	}
	if minioObject != nil {
		_ = minioObject.Close()
	}

	r2Adapter := &R2Adapter{client: client, bucketName: "bucket", baseURL: "https://cdn.example.com"}
	r2Header := &multipart.FileHeader{
		Filename: "hello.txt",
		Size:     int64(len("hello")),
		Header:   textproto.MIMEHeader{"Content-Type": []string{"text/plain"}},
	}
	if _, err := r2Adapter.UploadFile(ctx, storageTestMultipartFile{bytes.NewReader([]byte("hello"))}, r2Header, "docs"); err == nil {
		t.Fatal("expected r2 multipart upload error")
	}
	if _, err := r2Adapter.UploadFileFromBytes(ctx, []byte("hello"), "hello.txt", "docs", ""); err == nil {
		t.Fatal("expected r2 upload error")
	}
	if err := r2Adapter.DeleteFile(ctx, "https://cdn.example.com/docs/hello.txt"); err == nil {
		t.Fatal("expected r2 delete error")
	}
	r2Object, err := r2Adapter.DownloadFile(ctx, "docs/hello.txt")
	if err != nil {
		t.Fatalf("expected r2 download object creation, got %v", err)
	}
	if r2Object != nil {
		_ = r2Object.Close()
	}
}
