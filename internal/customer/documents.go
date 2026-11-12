package customer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"

	"backend-loanbanking/internal/imagehost"
)

const MaxImageBytes = 5 * 1024 * 1024

var (
	ErrInvalidDocument     = errors.New("unsupported customer document type")
	ErrInvalidImage        = errors.New("use a valid JPEG or PNG image")
	ErrImageTooLarge       = errors.New("image exceeds 5 MiB")
	ErrUploadNotConfigured = errors.New("image upload is not configured")
)

type DocumentKind string

const (
	KTPImage     DocumentKind = "ktp_image"
	SelfieImage  DocumentKind = "selfie_image"
	PayslipImage DocumentKind = "payslip_image"
)

func (kind DocumentKind) Valid() bool {
	return kind == KTPImage || kind == SelfieImage || kind == PayslipImage
}

type ImageUploader interface {
	Upload(context.Context, string, []byte) (imagehost.Image, error)
}

type Option func(*Service)

func WithImageUploader(uploader ImageUploader) Option {
	return func(service *Service) { service.uploader = uploader }
}

func (service *Service) UploadDocument(ctx context.Context, userID int64, kind DocumentKind, contents []byte) (Profile, error) {
	if userID <= 0 {
		return Profile{}, ErrUnauthenticated
	}
	if !kind.Valid() {
		return Profile{}, ErrInvalidDocument
	}
	if service.uploader == nil {
		return Profile{}, ErrUploadNotConfigured
	}
	if len(contents) > MaxImageBytes {
		return Profile{}, ErrImageTooLarge
	}
	mime := http.DetectContentType(contents)
	if mime != "image/jpeg" && mime != "image/png" {
		return Profile{}, ErrInvalidImage
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(contents))
	if err != nil || (format != "jpeg" && format != "png") || config.Width <= 0 || config.Height <= 0 || config.Width > 10000 || config.Height > 10000 || int64(config.Width)*int64(config.Height) > 25000000 {
		return Profile{}, ErrInvalidImage
	}
	// Check profile ownership before sending any bytes to the external provider.
	profile, err := service.repository.FindByUserID(ctx, userID)
	if err != nil {
		return Profile{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Profile{}, errors.New("cannot create upload name")
	}
	extension := ".jpg"
	if mime == "image/png" {
		extension = ".png"
	}
	name := fmt.Sprintf("customer_%d_%s_%s%s", profile.ID, kind, hex.EncodeToString(nonce[:]), extension)
	uploaded, err := service.uploader.Upload(ctx, name, contents)
	if err != nil {
		return Profile{}, imagehost.ErrUploadFailed
	}
	return service.repository.SaveDocument(ctx, userID, kind, uploaded)
}
