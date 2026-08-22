package control

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"cloud.google.com/go/storage"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type GCSBlobStore struct {
	client     *storage.Client
	bucket     *storage.BucketHandle
	bucketName string
	httpClient *http.Client
}

func NewGCSBlobStore(ctx context.Context, client *storage.Client, bucketName string) (*GCSBlobStore, error) {
	credentials, err := google.FindDefaultCredentials(ctx, storage.ScopeReadWrite)
	if err != nil {
		return nil, fmt.Errorf("load storage credentials: %w", err)
	}
	return &GCSBlobStore{
		client: client, bucket: client.Bucket(bucketName), bucketName: bucketName,
		httpClient: oauth2.NewClient(ctx, credentials.TokenSource),
	}, nil
}

func (s *GCSBlobStore) CreateUploadSession(ctx context.Context, object string, size int64, _ time.Time) (string, error) {
	endpoint := "https://storage.googleapis.com/upload/storage/v1/b/" + url.PathEscape(s.bucketName) +
		"/o?uploadType=resumable&name=" + url.QueryEscape(object)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	request.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
	request.Header.Set("X-Goog-If-Generation-Match", "0")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("storage resumable upload returned %s", response.Status)
	}
	session := response.Header.Get("Location")
	if session == "" {
		return "", fmt.Errorf("storage resumable upload did not return a session URL")
	}
	return session, nil
}

func (s *GCSBlobStore) DownloadURL(_ context.Context, object string, expires time.Time) (string, error) {
	return s.bucket.SignedURL(object, &storage.SignedURLOptions{
		Scheme: storage.SigningSchemeV4, Method: http.MethodGet, Expires: expires,
	})
}

func (s *GCSBlobStore) DeleteURL(_ context.Context, object string, expires time.Time) (string, error) {
	return s.bucket.SignedURL(object, &storage.SignedURLOptions{
		Scheme: storage.SigningSchemeV4, Method: http.MethodDelete, Expires: expires,
	})
}

func (s *GCSBlobStore) Delete(ctx context.Context, object string) error {
	err := s.bucket.Object(object).Delete(ctx)
	if err == storage.ErrObjectNotExist {
		return nil
	}
	return err
}
