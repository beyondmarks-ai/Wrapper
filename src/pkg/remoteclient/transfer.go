package remoteclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const uploadChunkSize = int64(8 << 20)

type ProgressFunc func(done, total int64)

func UploadResumable(ctx context.Context, httpClient *http.Client, sessionURL, path string, progress ProgressFunc) error {
	if err := requireHTTPS(sessionURL); err != nil {
		return err
	}
	httpClient = secureTransferClient(httpClient)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	total := info.Size()
	var offset int64
	stalled := 0
	for offset < total {
		end := min(offset+uploadChunkSize, total) - 1
		length := end - offset + 1
		section := io.NewSectionReader(file, offset, length)
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPut, sessionURL, section)
		if requestErr != nil {
			return requestErr
		}
		request.ContentLength = length
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end, total))
		response, requestErr := httpClient.Do(request)
		if requestErr != nil {
			resumed, resumeErr := queryUploadOffset(ctx, httpClient, sessionURL, total)
			if resumeErr != nil {
				return fmt.Errorf("upload interrupted (%v) and resume check failed: %w", requestErr, resumeErr)
			}
			if resumed <= offset {
				stalled++
				if stalled >= maxTransferRetries {
					return fmt.Errorf("upload made no progress after %d retries: %w", stalled, requestErr)
				}
				if err = waitForRetry(ctx, stalled); err != nil {
					return err
				}
			} else {
				stalled = 0
			}
			offset = resumed
			continue
		}
		response.Body.Close()
		previousOffset := offset
		if response.StatusCode == http.StatusPermanentRedirect || response.StatusCode == 308 {
			offset = parseUploadedOffset(response.Header.Get("Range"))
		} else if response.StatusCode >= 200 && response.StatusCode < 300 {
			offset = total
		} else {
			return fmt.Errorf("resumable upload returned %s", response.Status)
		}
		if offset <= previousOffset {
			return errors.New("resumable upload server reported no progress")
		}
		stalled = 0
		if progress != nil {
			progress(offset, total)
		}
	}
	return nil
}

func DownloadResumable(ctx context.Context, httpClient *http.Client, signedURL, path string, expected int64,
	progress ProgressFunc,
) error {
	if err := requireHTTPS(signedURL); err != nil {
		return err
	}
	httpClient = secureTransferClient(httpClient)
	var offset int64
	if info, err := os.Stat(path); err == nil {
		offset = info.Size()
	}
	if expected > 0 && offset == expected {
		if progress != nil {
			progress(offset, expected)
		}
		return nil
	}
	if expected > 0 && offset > expected {
		offset = 0
		if err := os.Truncate(path, 0); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return err
	}

	failures := 0
	for expected <= 0 || offset < expected {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, signedURL, nil)
		if requestErr != nil {
			return requestErr
		}
		if offset > 0 {
			request.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		}
		response, requestErr := httpClient.Do(request)
		if requestErr != nil {
			failures++
			if failures >= maxTransferRetries {
				return fmt.Errorf("download interrupted after %d retries: %w", failures, requestErr)
			}
			if err = waitForRetry(ctx, failures); err != nil {
				return err
			}
			continue
		}
		if offset > 0 && response.StatusCode == http.StatusOK {
			if err = file.Truncate(0); err == nil {
				_, err = file.Seek(0, io.SeekStart)
			}
			offset = 0
		} else if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
			response.Body.Close()
			return fmt.Errorf("download returned %s", response.Status)
		}
		if err != nil {
			response.Body.Close()
			return err
		}

		progressed := false
		buffer := make([]byte, 1024*1024)
		for {
			if err = ctx.Err(); err != nil {
				response.Body.Close()
				return err
			}
			count, readErr := response.Body.Read(buffer)
			if count > 0 {
				written, writeErr := file.Write(buffer[:count])
				offset += int64(written)
				progressed = true
				if writeErr != nil {
					response.Body.Close()
					return writeErr
				}
				if written != count {
					response.Body.Close()
					return io.ErrShortWrite
				}
				if expected > 0 && offset > expected {
					response.Body.Close()
					return fmt.Errorf("download size mismatch: received more than %d bytes", expected)
				}
				if progress != nil {
					progress(offset, expected)
				}
			}
			if errors.Is(readErr, io.EOF) {
				break
			}
			if readErr != nil {
				break
			}
		}
		response.Body.Close()
		if expected <= 0 || offset == expected {
			break
		}
		if progressed {
			failures = 0
		} else {
			failures++
		}
		if failures >= maxTransferRetries {
			return fmt.Errorf("download size mismatch after retries: received %d of %d bytes", offset, expected)
		}
		if err = waitForRetry(ctx, failures+1); err != nil {
			return err
		}
	}
	return file.Sync()
}
func queryUploadOffset(ctx context.Context, httpClient *http.Client, sessionURL string, total int64) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, sessionURL, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Content-Range", fmt.Sprintf("bytes */%d", total))
	request.ContentLength = 0
	response, err := httpClient.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusPermanentRedirect || response.StatusCode == 308 {
		return parseUploadedOffset(response.Header.Get("Range")), nil
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return total, nil
	}
	return 0, fmt.Errorf("upload status query returned %s", response.Status)
}

func parseUploadedOffset(value string) int64 {
	value = strings.TrimPrefix(value, "bytes=")
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0
	}
	last, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0
	}
	return last + 1
}

func requireHTTPS(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("transfer URL must use HTTPS")
	}
	return nil
}

const maxTransferRetries = 5

func waitForRetry(ctx context.Context, attempt int) error {
	delay := min(time.Duration(1<<min(attempt, 3))*100*time.Millisecond, 2*time.Second)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func secureTransferClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 35 * time.Second}
	}
	clone := *client
	previous := client.CheckRedirect
	clone.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if err := requireHTTPS(request.URL.String()); err != nil {
			return err
		}
		if previous != nil {
			return previous(request, via)
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	}
	return &clone
}
