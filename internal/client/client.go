// Package client is a small client for the AppSignal GraphQL API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultEndpoint = "https://appsignal.com/graphql"

type Client struct {
	endpoint  string
	token     string
	userAgent string
	http      *http.Client
	// retryWait is the pause before retrying a rate-limited request.
	retryWait time.Duration
}

func New(endpoint, token, userAgent string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		endpoint:  endpoint,
		token:     token,
		userAgent: userAgent,
		http:      &http.Client{Timeout: 60 * time.Second},
		retryWait: 5 * time.Second,
	}
}

// Error carries the messages of a GraphQL error response.
type Error struct {
	Messages []string
}

func (e *Error) Error() string {
	return "appsignal: " + strings.Join(e.Messages, "; ")
}

// IsNotFound reports whether the API said the requested object does not exist.
func IsNotFound(err error) bool {
	var e *Error
	if !errors.As(err, &e) {
		return false
	}
	for _, m := range e.Messages {
		if m == "Object not found." {
			return true
		}
	}
	return false
}

// Do runs one GraphQL operation and decodes its data into out.
func (c *Client) Do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return fmt.Errorf("appsignal: invalid endpoint: %w", err)
	}
	q := u.Query()
	q.Set("token", c.token)
	u.RawQuery = q.Encode()

	var raw []byte
	var status int
	for attempt := 0; ; attempt++ {
		raw, status, err = c.post(ctx, u.String(), body)
		if err != nil {
			return err
		}
		// A 429 means the request was refused, so a retry cannot apply a mutation twice.
		if status != http.StatusTooManyRequests || attempt == 3 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.retryWait * time.Duration(attempt+1)):
		}
	}

	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if jsonErr := json.Unmarshal(raw, &env); jsonErr != nil || (status != http.StatusOK && len(env.Errors) == 0) {
		return fmt.Errorf("appsignal: HTTP %d: %s", status, snippet(raw))
	}
	if len(env.Errors) > 0 {
		e := &Error{}
		for _, m := range env.Errors {
			e.Messages = append(e.Messages, m.Message)
		}
		return e
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	return json.Unmarshal(env.Data, out)
}

func (c *Client) post(ctx context.Context, target string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, 0, errors.New("appsignal: could not build request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A *url.Error repeats the request URL, and with it the token.
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, 0, fmt.Errorf("appsignal: %s request failed: %w", ue.Op, ue.Err)
		}
		return nil, 0, errors.New("appsignal: request failed")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("appsignal: reading response: %w", err)
	}
	return raw, resp.StatusCode, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
