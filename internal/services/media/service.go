package servicemedia

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"starter-kit/internal/authscope"
	domainmedia "starter-kit/internal/domain/media"
	interfacemedia "starter-kit/internal/interfaces/media"
	"starter-kit/pkg/storage"
	"starter-kit/utils"
)

const (
	defaultMaxFileSize = int64(10 << 20)
	mediaFolder        = "media"
)

var defaultContentTypes = []string{
	"application/pdf",
	"image/gif",
	"image/jpeg",
	"image/png",
	"image/webp",
}

type MediaService struct {
	repo                interfacemedia.RepoMediaInterface
	storage             storage.StorageProvider
	maxFileSize         int64
	allowedContentTypes map[string]struct{}
}

func NewMediaService(repo interfacemedia.RepoMediaInterface, storageProvider storage.StorageProvider, maxFileSize int64, allowedContentTypes []string) *MediaService {
	if maxFileSize <= 0 {
		maxFileSize = defaultMaxFileSize
	}
	if len(allowedContentTypes) == 0 {
		allowedContentTypes = defaultContentTypes
	}

	allowed := make(map[string]struct{}, len(allowedContentTypes))
	for _, contentType := range allowedContentTypes {
		if normalized := normalizeContentType(contentType); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}

	return &MediaService{
		repo:                repo,
		storage:             storageProvider,
		maxFileSize:         maxFileSize,
		allowedContentTypes: allowed,
	}
}

func (s *MediaService) MaxFileSize() int64 {
	return s.maxFileSize
}

func (s *MediaService) Upload(ctx context.Context, ownerUserID string, file multipart.File, header *multipart.FileHeader) (domainmedia.Media, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if _, err := uuid.Parse(ownerUserID); err != nil {
		return domainmedia.Media{}, domainmedia.ErrMediaForbidden
	}
	if file == nil || header == nil || header.Size <= 0 {
		return domainmedia.Media{}, domainmedia.ErrEmptyFile
	}
	if header.Size > s.maxFileSize {
		return domainmedia.Media{}, domainmedia.ErrFileTooLarge
	}

	contentType, err := detectContentType(file)
	if err != nil {
		return domainmedia.Media{}, err
	}
	if _, ok := s.allowedContentTypes[contentType]; !ok {
		return domainmedia.Media{}, domainmedia.ErrUnsupportedContentType
	}

	uploadHeader := *header
	uploadHeader.Header = make(textproto.MIMEHeader)
	uploadHeader.Header.Set("Content-Type", contentType)
	uploadHeader.Filename = "upload" + extensionForContentType(contentType)

	uploaded, err := s.storage.UploadFile(ctx, file, &uploadHeader, mediaFolder)
	if err != nil {
		return domainmedia.Media{}, err
	}

	media := domainmedia.Media{
		ID:           utils.CreateUUID(),
		OwnerUserID:  ownerUserID,
		ObjectKey:    uploaded.ObjectName,
		URL:          uploaded.URL,
		OriginalName: safeFilename(header.Filename),
		ContentType:  contentType,
		Size:         header.Size,
		CreatedAt:    time.Now(),
	}
	if err := s.repo.Store(ctx, media); err != nil {
		cleanupErr := s.storage.DeleteFile(ctx, uploaded.URL)
		return domainmedia.Media{}, errors.Join(err, cleanupErr)
	}

	return media, nil
}

func (s *MediaService) Delete(ctx context.Context, scope authscope.Scope, id string) (domainmedia.Media, error) {
	media, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return domainmedia.Media{}, err
	}
	if media.OwnerUserID != strings.TrimSpace(scope.UserID) && strings.TrimSpace(scope.Role) != utils.RoleSuperAdmin {
		return domainmedia.Media{}, domainmedia.ErrMediaForbidden
	}

	if err := s.storage.DeleteFile(ctx, media.URL); err != nil {
		return domainmedia.Media{}, err
	}
	if err := s.repo.Delete(ctx, media.ID); err != nil {
		return domainmedia.Media{}, err
	}

	return media, nil
}

func detectContentType(file multipart.File) (string, error) {
	buffer := make([]byte, 512)
	n, err := io.ReadFull(file, buffer)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	if n == 0 {
		return "", domainmedia.ErrEmptyFile
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	return normalizeContentType(http.DetectContentType(buffer[:n])), nil
}

func normalizeContentType(contentType string) string {
	contentType, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(contentType)), ";")
	return strings.TrimSpace(contentType)
}

func extensionForContentType(contentType string) string {
	switch contentType {
	case "application/pdf":
		return ".pdf"
	case "image/gif":
		return ".gif"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func safeFilename(filename string) string {
	return path.Base(strings.ReplaceAll(strings.TrimSpace(filename), "\\", "/"))
}

var _ interfacemedia.ServiceMediaInterface = (*MediaService)(nil)
