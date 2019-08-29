package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/stretchr/testify/require"
)

const (
	logzioTestToken = "123456789"
)

func TestClientWithDefaults(t *testing.T) {
	c, err := NewClient(logzioTestToken)
	require.NoError(t, err)
	require.Equal(t, c.url, defaultURL)
	require.Equal(t, c.logger.debug, false)
}

func TestRequestHeaders(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c, err := NewClient(logzioTestToken,
		SetURL(ts.URL),
	)
	require.NoError(t, err)

	res := c.Send([]byte("test"))
	require.Equal(t, res, output.FLB_OK)
}

func TestStatusCodeResponses(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()

	u, err := url.Parse(fmt.Sprintf("http://%s", ts.Listener.Addr().String()))
	require.NoError(t, err)

	tests := []struct {
		name       string
		client     *LogzioClient
		statusCode int
		errFunc    func(t *testing.T, res int)
	}{
		{
			name: "success",
			client: &LogzioClient{
				token:     logzioTestToken,
				url:       u.String(),
				logger:    NewLogger("testing", true),
				client:    http.DefaultClient,
				threshold: maxRequestBodySize,
			},
			statusCode: http.StatusOK,
			errFunc: func(t *testing.T, res int) {
				require.Equal(t, res, output.FLB_OK)
			},
		},
		{
			name: "1xx status is an error",
			client: &LogzioClient{
				token:     logzioTestToken,
				url:       u.String(),
				logger:    NewLogger("testing", true),
				client:    http.DefaultClient,
				threshold: maxRequestBodySize,
			},
			statusCode: http.StatusSwitchingProtocols,
			errFunc: func(t *testing.T, res int) {
				require.Equal(t, res, output.FLB_ERROR)
			},
		},
		{
			name: "3xx status is an error",
			client: &LogzioClient{
				token:     logzioTestToken,
				url:       u.String(),
				logger:    NewLogger("testing", true),
				client:    http.DefaultClient,
				threshold: maxRequestBodySize,
			},
			statusCode: http.StatusMultipleChoices,
			errFunc: func(t *testing.T, res int) {
				require.Equal(t, res, output.FLB_ERROR)
			},
		},
		{
			name: "4xx status is an error",
			client: &LogzioClient{
				token:     logzioTestToken,
				url:       u.String(),
				logger:    NewLogger("testing", true),
				client:    http.DefaultClient,
				threshold: maxRequestBodySize,
			},
			statusCode: http.StatusForbidden,
			errFunc: func(t *testing.T, res int) {
				require.Equal(t, res, output.FLB_ERROR)
			},
		},
		{
			name: "5xx status is an error",
			client: &LogzioClient{
				token:     logzioTestToken,
				url:       u.String(),
				logger:    NewLogger("testing", true),
				client:    http.DefaultClient,
				threshold: maxRequestBodySize,
			},
			statusCode: http.StatusInternalServerError,
			errFunc: func(t *testing.T, res int) {
				require.Equal(t, res, output.FLB_RETRY)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			tt.client.Send([]byte("test"))
			res := tt.client.Flush()
			tt.errFunc(t, res)
		})
	}
}

func TestBulkSending(t *testing.T) {
	bulks := 3
	ts := httptest.NewServer(http.NotFoundHandler())
	ts.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logs, err := readLogs(r)
		require.NoError(t, err)
		require.Len(t, logs, 1) // each log is one bulk
		w.WriteHeader(http.StatusOK)
	})
	defer ts.Close()

	c, err := NewClient(logzioTestToken,
		SetURL(ts.URL),
		SetDebug(true),
		SetBodySizeThreshold(0),
	)
	require.NoError(t, err)

	for i := 1; i <= bulks; i++ {
		msg := fmt.Sprintf("bulk - %d", i)
		ok := c.Send([]byte(msg))
		require.Equal(t, ok, output.FLB_OK)
	}

	c.Flush()
}

func readLogs(r *http.Request) ([]string, error) {
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(gz)

	logs := make([]string, 0)
	for scanner.Scan() {
		line := scanner.Text()
		logs = append(logs, line)
	}

	return logs, nil
}
