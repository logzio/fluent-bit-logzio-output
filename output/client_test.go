package main

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/stretchr/testify/require"
)

const (
	logzioTestToken = "123456789"
)

func TestClientWithDefaults(test *testing.T) {
	logzioClient, err := NewClient(logzioTestToken)
	require.NoError(test, err)
	require.Equal(test, logzioClient.listenerURL, defaultURL)
	require.Equal(test, logzioClient.logger.debug, false)
	require.Equal(test, logzioClient.sizeThresholdInBytes, defaultSizeThresholdMB*megaByte)
}

func TestClientSetBodySizeThresholdMBValid(test *testing.T) {
	customThresholdMB := 5
	logzioClient, err := NewClient(logzioTestToken, SetBodySizeThresholdMB(customThresholdMB))
	require.NoError(test, err)
	require.Equal(test, logzioClient.sizeThresholdInBytes, customThresholdMB*megaByte)
}

func TestClientSetBodySizeThresholdMBTooLow(test *testing.T) {
	customThresholdMB := 0 // Below minSizeThresholdMB
	logzioClient, err := NewClient(logzioTestToken, SetBodySizeThresholdMB(customThresholdMB))
	require.NoError(test, err)
	require.Equal(test, logzioClient.sizeThresholdInBytes, defaultSizeThresholdMB*megaByte)
}

func TestClientSetBodySizeThresholdMBTooHigh(test *testing.T) {
	customThresholdMB := 15 // Above maxSizeThresholdMB
	logzioClient, err := NewClient(logzioTestToken, SetBodySizeThresholdMB(customThresholdMB))
	require.NoError(test, err)
	require.Equal(test, logzioClient.sizeThresholdInBytes, defaultSizeThresholdMB*megaByte)
}

func TestRequestHeaders(test *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(test, "gzip", r.Header.Get("Content-Encoding"))
		require.Equal(test, "application/json", r.Header.Get("Content-Type"))
		require.Equal(test, "header_value_1", r.Header.Get("X-Custom-Header1"))
		require.Equal(test, "header_value_2", r.Header.Get("X-Custom-Header2"))
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()
	headers := map[string]string{
		"X-Custom-Header1": "header_value_1",
		"X-Custom-Header2": "header_value_2",
	}
	logzioClient, err := NewClient(logzioTestToken, SetURL(testServer.URL), SetHeaders(headers))
	require.NoError(test, err)
	res := logzioClient.Send([]byte("test"))
	require.Equal(test, res, output.FLB_OK)
	res = logzioClient.Flush()
	require.Equal(test, res, output.FLB_OK)
}

func TestNoHeaders(test *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(test, "gzip", r.Header.Get("Content-Encoding"))
		require.Equal(test, "application/json", r.Header.Get("Content-Type"))
		require.Equal(test, "", r.Header.Get("X-Custom-Header1"))
		require.Equal(test, "", r.Header.Get("X-Custom-Header2"))
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()
	logzioClient, err := NewClient(logzioTestToken, SetURL(testServer.URL))
	require.NoError(test, err)
	res := logzioClient.Send([]byte("test"))
	require.Equal(test, res, output.FLB_OK)
	res = logzioClient.Flush()
	require.Equal(test, res, output.FLB_OK)
}

func TestStatusOKCodeResponse(test *testing.T) {
	doStatusCodeResponseTest(test, http.StatusOK, output.FLB_OK)
}
func Test1xxStatusCodeResponse(test *testing.T) {
	doStatusCodeResponseTest(test, http.StatusSwitchingProtocols, output.FLB_ERROR)
}
func Test3xxStatusCodeResponse(test *testing.T) {
	doStatusCodeResponseTest(test, http.StatusMultipleChoices, output.FLB_ERROR)
}
func Test4xxStatusCodeResponse(test *testing.T) {
	doStatusCodeResponseTest(test, http.StatusForbidden, output.FLB_ERROR)
}
func Test5xxStatusCodeResponse(test *testing.T) {
	doStatusCodeResponseTest(test, http.StatusInternalServerError, output.FLB_RETRY)
}

func TestBulkSendingWithSmallThreshold(test *testing.T) {
	sendCount := 3
	receiveCount := 0
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logs, err := readLogs(r)
		require.NoError(test, err)
		require.NotEmpty(test, logs)
		receiveCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer testServer.Close()

	// Use the minimum valid threshold (1MB)
	logzioClient, err := NewClient(logzioTestToken,
		SetURL(testServer.URL),
		SetDebug(true),
		SetBodySizeThresholdMB(minSizeThresholdMB), 
	)
	require.NoError(test, err)
	require.Equal(test, minSizeThresholdMB*megaByte, logzioClient.sizeThresholdInBytes) 

	for i := 1; i <= sendCount; i++ {
		msg := fmt.Sprintf("bulk - %d", i)
		ok := logzioClient.Send([]byte(msg))
		require.Equal(test, ok, output.FLB_OK)
	}
	res := logzioClient.Flush()
	require.Equal(test, res, output.FLB_OK)
	require.GreaterOrEqual(test, receiveCount, 1) 
}

func readLogs(r *http.Request) ([]string, error) {
	gzipReader, err := gzip.NewReader(r.Body)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	defer gzipReader.Close()
	scanner := bufio.NewScanner(gzipReader)
	logs := make([]string, 0)
	for scanner.Scan() {
		logs = append(logs, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading logs: %w", err)
	}
	return logs, nil
}

func doStatusCodeResponseTest(test *testing.T, statusCode int, expectedReturnCode int) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
	}))
	defer testServer.Close()
	logzioClient := LogzioTestClient(testServer.URL) 
	res := logzioClient.Send([]byte("test"))
	require.Equal(test, res, output.FLB_OK)
	res = logzioClient.Flush()
	require.Equal(test, res, expectedReturnCode)
}

func LogzioTestClient(testURL string) *LogzioClient {
	// Uses default threshold now implicitly via NewClient
	client, err := NewClient(logzioTestToken, SetURL(testURL), SetDebug(true))
	if err != nil {
		panic(fmt.Sprintf("LogzioTestClient failed: %v", err)) 
	}
	client.client = &http.Client{Timeout: 5 * time.Second} 
	return client
}