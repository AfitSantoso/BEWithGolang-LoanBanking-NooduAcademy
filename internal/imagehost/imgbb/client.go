package imgbb

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend-loanbanking/internal/imagehost"
)

var imageIDPattern = regexp.MustCompile(`^[a-zA-Z0-9]{1,64}$`)

type Client struct {
	key        string
	endpoint   string
	httpClient *http.Client
	expiration int
}

type Option func(*Client)

// WithExpiration is optional; production uploads do not expire by default.
// A short lifetime is useful when verifying the provider using synthetic images.
func WithExpiration(seconds int) Option { return func(client *Client) { client.expiration = seconds } }

func NewClient(key string, options ...Option) *Client {
	client := &Client{key: key, endpoint: "https://api.imgbb.com/1/upload", httpClient: &http.Client{
		Timeout: 20 * time.Second,
		// Do not forward credentials or document bytes to redirected destinations.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	for _, option := range options {
		option(client)
	}
	return client
}

func (client *Client) Upload(ctx context.Context, name string, contents []byte) (imagehost.Image, error) {
	fail := func() (imagehost.Image, error) { return imagehost.Image{}, imagehost.ErrUploadFailed }
	if client.key == "" || (client.expiration != 0 && (client.expiration < 60 || client.expiration > 15552000)) {
		return fail()
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Send credentials in the form, never in the request URL or application logs.
	if writer.WriteField("key", client.key) != nil || writer.WriteField("name", name) != nil {
		return fail()
	}
	if client.expiration != 0 && writer.WriteField("expiration", strconv.Itoa(client.expiration)) != nil {
		return fail()
	}
	part, err := writer.CreateFormFile("image", name)
	if err != nil {
		return fail()
	}
	if _, err = part.Write(contents); err != nil {
		return fail()
	}
	if writer.Close() != nil {
		return fail()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, &body)
	if err != nil {
		return fail()
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fail()
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fail()
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return fail()
	}
	var result struct {
		Data struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"data"`
		Success bool `json:"success"`
		Status  int  `json:"status"`
	}
	if json.Unmarshal(data, &result) != nil || !result.Success || result.Status != 200 || !imageIDPattern.MatchString(result.Data.ID) {
		return fail()
	}
	imageURL, err := url.Parse(result.Data.URL)
	if err != nil || imageURL.Scheme != "https" || imageURL.Host != "i.ibb.co" || imageURL.User != nil || imageURL.RawQuery != "" || imageURL.Fragment != "" || strings.Trim(imageURL.Path, "/") == "" || len(result.Data.URL) > 255 {
		return fail()
	}
	return imagehost.Image{ID: result.Data.ID, URL: result.Data.URL}, nil
}
