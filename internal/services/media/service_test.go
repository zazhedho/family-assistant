package servicemedia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/textproto"
	"testing"

	"github.com/google/uuid"

	"starter-kit/internal/authscope"
	domainmedia "starter-kit/internal/domain/media"
	"starter-kit/pkg/storage"
	"starter-kit/utils"
)

type mediaTestFile struct {
	*bytes.Reader
}

func (mediaTestFile) Close() error { return nil }

type mediaRepoMock struct {
	stored    domainmedia.Media
	storeErr  error
	media     domainmedia.Media
	getErr    error
	deletedID string
	deleteErr error
}

func (m *mediaRepoMock) Store(_ context.Context, media domainmedia.Media) error {
	m.stored = media
	return m.storeErr
}

func (m *mediaRepoMock) GetByID(_ context.Context, _ string) (domainmedia.Media, error) {
	return m.media, m.getErr
}

func (m *mediaRepoMock) Delete(_ context.Context, id string) error {
	m.deletedID = id
	return m.deleteErr
}

type mediaStorageMock struct {
	uploadedHeader *multipart.FileHeader
	uploadResult   storage.FileInfo
	uploadErr      error
	deletedURL     string
	deleteErr      error
}

func (m *mediaStorageMock) UploadFile(_ context.Context, _ multipart.File, header *multipart.FileHeader, _ string) (storage.FileInfo, error) {
	m.uploadedHeader = header
	return m.uploadResult, m.uploadErr
}

func (m *mediaStorageMock) UploadFileFromBytes(context.Context, []byte, string, string, string) (storage.FileInfo, error) {
	return storage.FileInfo{}, errors.New("not implemented")
}

func (m *mediaStorageMock) DeleteFile(_ context.Context, fileURL string) error {
	m.deletedURL = fileURL
	return m.deleteErr
}

func (m *mediaStorageMock) GetFileURL(objectName string) string { return objectName }

func (m *mediaStorageMock) DownloadFile(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func newMediaHeader(filename string, size int64) *multipart.FileHeader {
	return &multipart.FileHeader{Filename: filename, Size: size, Header: textproto.MIMEHeader{}}
}

func TestUploadDetectsContentAndStoresMetadata(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	repo := &mediaRepoMock{}
	storageMock := &mediaStorageMock{uploadResult: storage.FileInfo{ObjectName: "media/generated.png", URL: "https://cdn/media/generated.png"}}
	service := NewMediaService(repo, storageMock, 1024, nil)
	ownerID := uuid.NewString()

	media, err := service.Upload(context.Background(), ownerID, mediaTestFile{bytes.NewReader(png)}, newMediaHeader("../avatar.exe", int64(len(png))))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if media.OwnerUserID != ownerID || media.ObjectKey != "media/generated.png" || media.OriginalName != "avatar.exe" {
		t.Fatalf("unexpected media metadata: %+v", media)
	}
	if media.ContentType != "image/png" || storageMock.uploadedHeader.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("expected detected image/png, got media=%q header=%q", media.ContentType, storageMock.uploadedHeader.Header.Get("Content-Type"))
	}
	if storageMock.uploadedHeader.Filename != "upload.png" {
		t.Fatalf("expected server-controlled filename, got %q", storageMock.uploadedHeader.Filename)
	}
}

func TestUploadRejectsInvalidFilesBeforeStorage(t *testing.T) {
	ownerID := uuid.NewString()
	tests := []struct {
		name    string
		data    []byte
		header  *multipart.FileHeader
		maxSize int64
		wantErr error
	}{
		{name: "empty", header: newMediaHeader("empty.png", 0), maxSize: 10, wantErr: domainmedia.ErrEmptyFile},
		{name: "too large", data: []byte("large"), header: newMediaHeader("large.png", 6), maxSize: 5, wantErr: domainmedia.ErrFileTooLarge},
		{name: "unsupported content", data: []byte("plain text"), header: newMediaHeader("fake.png", 10), maxSize: 20, wantErr: domainmedia.ErrUnsupportedContentType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageMock := &mediaStorageMock{}
			service := NewMediaService(&mediaRepoMock{}, storageMock, tt.maxSize, nil)
			_, err := service.Upload(context.Background(), ownerID, mediaTestFile{bytes.NewReader(tt.data)}, tt.header)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
			if storageMock.uploadedHeader != nil {
				t.Fatal("storage must not be called for rejected file")
			}
		})
	}
}

func TestUploadDeletesObjectWhenMetadataStoreFails(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	repoErr := errors.New("database unavailable")
	repo := &mediaRepoMock{storeErr: repoErr}
	storageMock := &mediaStorageMock{uploadResult: storage.FileInfo{ObjectName: "media/generated.png", URL: "https://cdn/media/generated.png"}}
	service := NewMediaService(repo, storageMock, 1024, nil)

	_, err := service.Upload(context.Background(), uuid.NewString(), mediaTestFile{bytes.NewReader(png)}, newMediaHeader("avatar.png", int64(len(png))))
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repository error, got %v", err)
	}
	if storageMock.deletedURL != storageMock.uploadResult.URL {
		t.Fatalf("expected uploaded object cleanup, got %q", storageMock.deletedURL)
	}
}

func TestDeleteEnforcesOwnership(t *testing.T) {
	ownerID := uuid.NewString()
	media := domainmedia.Media{ID: uuid.NewString(), OwnerUserID: ownerID, URL: "https://cdn/media/file.png"}

	t.Run("owner", func(t *testing.T) {
		repo := &mediaRepoMock{media: media}
		storageMock := &mediaStorageMock{}
		service := NewMediaService(repo, storageMock, 1024, nil)

		if _, err := service.Delete(context.Background(), authscope.New(ownerID, "owner", "viewer", nil), media.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if storageMock.deletedURL != media.URL || repo.deletedID != media.ID {
			t.Fatalf("expected storage and metadata deletion, got url=%q id=%q", storageMock.deletedURL, repo.deletedID)
		}
	})

	t.Run("other user", func(t *testing.T) {
		repo := &mediaRepoMock{media: media}
		storageMock := &mediaStorageMock{}
		service := NewMediaService(repo, storageMock, 1024, nil)

		_, err := service.Delete(context.Background(), authscope.New(uuid.NewString(), "other", "viewer", nil), media.ID)
		if !errors.Is(err, domainmedia.ErrMediaForbidden) {
			t.Fatalf("expected forbidden, got %v", err)
		}
		if storageMock.deletedURL != "" || repo.deletedID != "" {
			t.Fatal("forbidden delete must not mutate storage or metadata")
		}
	})

	t.Run("superadmin", func(t *testing.T) {
		repo := &mediaRepoMock{media: media}
		storageMock := &mediaStorageMock{}
		service := NewMediaService(repo, storageMock, 1024, nil)

		if _, err := service.Delete(context.Background(), authscope.New(uuid.NewString(), "root", utils.RoleSuperAdmin, nil), media.ID); err != nil {
			t.Fatalf("delete as superadmin: %v", err)
		}
	})
}
