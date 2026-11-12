package customer

import (
	"backend-loanbanking/internal/imagehost"
	"context"
)

// Repository is the persistence contract consumed by Service.
type Repository interface {
	FindByUserID(context.Context, int64) (Profile, error)
	Save(context.Context, int64, SaveInput) (Profile, error)
	SaveDocument(context.Context, int64, DocumentKind, imagehost.Image) (Profile, error)
}
