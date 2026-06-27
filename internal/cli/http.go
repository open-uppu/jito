package cli

import (
	"net/http"
	"time"
)

// headURL does a HEAD request with custom timeout (seconds).
func headURL(url string, timeoutSec int) (int, error) {
	if timeoutSec == 0 {
		timeoutSec = 5
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	resp, err := client.Head(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}