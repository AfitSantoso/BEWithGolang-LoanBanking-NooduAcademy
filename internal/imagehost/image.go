package imagehost

import "errors"

var ErrUploadFailed = errors.New("image provider upload failed")

// Image contains only public image metadata; never an API key or deletion token.
type Image struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}
