package customer

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"

	"backend-loanbanking/internal/imagehost"
)

type documentRepo struct {
	fakeRepository
	profile              Profile
	findErr, saveErr     error
	findCalls, saveCalls int
	kind                 DocumentKind
	uploaded             imagehost.Image
}

func (repo *documentRepo) FindByUserID(_ context.Context, id int64) (Profile, error) {
	repo.findCalls++
	repo.userID = id
	return repo.profile, repo.findErr
}
func (repo *documentRepo) SaveDocument(_ context.Context, id int64, kind DocumentKind, uploaded imagehost.Image) (Profile, error) {
	repo.saveCalls++
	repo.userID = id
	repo.kind = kind
	repo.uploaded = uploaded
	return Profile{ID: 7, UserID: id}, repo.saveErr
}

type fakeUploader struct {
	calls int
	name  string
	err   error
}

func (u *fakeUploader) Upload(_ context.Context, name string, _ []byte) (imagehost.Image, error) {
	u.calls++
	u.name = name
	return imagehost.Image{ID: "abc123", URL: "https://i.ibb.co/abc/test.png"}, u.err
}
func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestUploadChecksOwnershipAndSavesMetadata(t *testing.T) {
	for _, kind := range []DocumentKind{KTPImage, SelfieImage, PayslipImage} {
		repo := &documentRepo{profile: Profile{ID: 7, UserID: 42}}
		uploader := &fakeUploader{}
		service := NewService(repo, WithImageUploader(uploader))
		profile, err := service.UploadDocument(context.Background(), 42, kind, testPNG(t))
		if err != nil || profile.UserID != 42 || repo.userID != 42 || repo.kind != kind || repo.uploaded.ID != "abc123" || repo.saveCalls != 1 || uploader.calls != 1 {
			t.Fatalf("upload failed: %v", err)
		}
		if !strings.HasPrefix(uploader.name, "customer_7_"+string(kind)+"_") || !strings.HasSuffix(uploader.name, ".png") {
			t.Fatal("upload name must use customer ID and type")
		}
		firstName := uploader.name
		if _, err := service.UploadDocument(context.Background(), 42, kind, testPNG(t)); err != nil {
			t.Fatal(err)
		}
		if uploader.name == firstName {
			t.Fatal("replacement must have a fresh random name")
		}
	}
}
func TestInvalidUploadDoesNotReachProvider(t *testing.T) {
	for _, tc := range []struct {
		name    string
		id      int64
		kind    DocumentKind
		data    []byte
		findErr error
		want    error
	}{
		{"missing identity", 0, KTPImage, testPNG(t), nil, ErrUnauthenticated},
		{"invalid kind", 42, "other", testPNG(t), nil, ErrInvalidDocument},
		{"fake image", 42, KTPImage, []byte("not an image"), nil, ErrInvalidImage},
		{"empty", 42, KTPImage, nil, nil, ErrInvalidImage},
		{"oversize", 42, KTPImage, make([]byte, MaxImageBytes+1), nil, ErrImageTooLarge},
		{"missing profile", 42, KTPImage, testPNG(t), ErrNotFound, ErrNotFound},
		{"database unavailable", 42, KTPImage, testPNG(t), context.DeadlineExceeded, context.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &documentRepo{findErr: tc.findErr}
			uploader := &fakeUploader{}
			service := NewService(repo, WithImageUploader(uploader))
			_, err := service.UploadDocument(context.Background(), tc.id, tc.kind, tc.data)
			if !errors.Is(err, tc.want) || uploader.calls != 0 || repo.saveCalls != 0 {
				t.Fatalf("invalid upload escaped: %v", err)
			}
		})
	}
}
func TestUploadFailureDoesNotSaveAndMissingConfigurationIsExplicit(t *testing.T) {
	repo := &documentRepo{profile: Profile{ID: 7}}
	uploader := &fakeUploader{err: errors.New("secret provider error")}
	_, err := NewService(repo, WithImageUploader(uploader)).UploadDocument(context.Background(), 42, KTPImage, testPNG(t))
	if !errors.Is(err, imagehost.ErrUploadFailed) || repo.saveCalls != 0 || strings.Contains(err.Error(), "secret") {
		t.Fatal("provider failure leaked/saved")
	}
	_, err = NewService(repo).UploadDocument(context.Background(), 42, KTPImage, testPNG(t))
	if !errors.Is(err, ErrUploadNotConfigured) {
		t.Fatal(err)
	}
	uploader.err = nil
	repo.saveErr = context.DeadlineExceeded
	_, err = NewService(repo, WithImageUploader(uploader)).UploadDocument(context.Background(), 42, KTPImage, testPNG(t))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
