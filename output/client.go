//go:build linux || darwin || windows
// +build linux darwin windows

package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"github.com/fluent/fluent-bit-go/output"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultURL                = "https://listener.logz.io:8071"
	defaultId                 = "logzio_output_1"
	maxRequestBodySizeInBytes = 9 * 1024 * 1024 // 9MB
	megaByte                  = 1 * 1024 * 1024 // 1MB
)

// LogzioClient http client that sends bulks to Logz.io http listener
type LogzioClient struct {
	listenerURL          string
	token                string
	bulk                 []byte
	client               *http.Client
	logger               *Logger
	sizeThresholdInBytes int
}

// ClientOptionFunc options for Logz.io
type ClientOptionFunc func(*LogzioClient) error

// NewClient is a constructor for Logz.io http client
func NewClient(token string, options ...ClientOptionFunc) (*LogzioClient, error) {
	logzioClient := &LogzioClient{
		listenerURL:          defaultURL,
		token:                token,
		logger:               NewLogger(outputName, false),
		sizeThresholdInBytes: maxRequestBodySizeInBytes,
	}
	tlsConfig := &tls.Config{}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		Proxy:           http.ProxyFromEnvironment, // proxy_url set in out_logzio.go
	}
	// in case server side is sleeping - wait 10s instead of waiting for him to wake up
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   time.Second * 10,
	}

	logzioClient.client = httpClient

	for _, option := range options {
		if err := option(logzioClient); err != nil {
			return nil, err
		}
	}

	return logzioClient, nil
}

// SetURL set the url which maybe different from the defaultUrl
func SetURL(listenerURL string) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		logzioClient.listenerURL = listenerURL
		logzioClient.logger.Debug(fmt.Sprintf("setting listener url to %s\n", listenerURL))
		return nil
	}
}

// SetDebug mode and send logs to this writer
func SetDebug(debug bool) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		logzioClient.logger.SetDebug(debug)
		logzioClient.logger.Debug(fmt.Sprintf("setting debug to %t\n", debug))
		return nil
	}
}

// SetBodySizeThreshold set the maximum body size of the client http request
// The param is in MB and can be between 0(mostly for testing) and 9
func SetBodySizeThreshold(threshold int) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		logzioClient.sizeThresholdInBytes = threshold * megaByte
		if threshold < 0 || threshold > 9 {
			logzioClient.logger.Debug("falling back to the default BodySizeThreshold")
			logzioClient.sizeThresholdInBytes = maxRequestBodySizeInBytes
		}
		logzioClient.logger.Debug(fmt.Sprintf("setting BodySizeThreshold to %d\n", logzioClient.sizeThresholdInBytes))
		return nil
	}
}

// SetProxy set the http proxy url
func SetProxy(proxyHost string, proxyUser string, proxyPass string) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		if proxyHost != "" {
			proxyURLStr := fmt.Sprintf("http://%s", proxyHost)

			if proxyUser != "" && proxyPass != "" {
				proxyURLStr = fmt.Sprintf("http://%s:%s@%s", proxyUser, proxyPass, proxyHost)
			}
			logzioClient.logger.Debug(fmt.Sprintf("setting http proxy url to %s\n", proxyURLStr))
			if proxyURLStr != "http://" && proxyURLStr != "http://:@" {
				proxyURL, err := url.Parse(proxyURLStr)
				if err != nil {
					fmt.Printf("Failed to set proxy url: %s.\nError:\n%s.", proxyURLStr, err)
				} else {

					transport := http.Transport{}
					transport.Proxy = http.ProxyURL(proxyURL) // set proxy
					logzioClient.client.Transport = &transport
				}
			}
		}

		return nil
	}
}

// Send adds the log to the client bulk slice check if we should send the bulk
func (logzioClient *LogzioClient) Send(log []byte) int {
	// Logz.io maximum request body size is 10MB. We send bulks that
	// exceed this size (with a safety buffer) via separate write requests.
	if (len(logzioClient.bulk) + len(log) + 1) > logzioClient.sizeThresholdInBytes {
		res := logzioClient.sendBulk()
		logzioClient.bulk = nil
		if res != output.FLB_OK {
			return res
		}
	}
	logzioClient.logger.Debug(fmt.Sprintf("adding log to the bulk: %+v\n", string(log)))
	logzioClient.bulk = append(logzioClient.bulk, log...)
	logzioClient.bulk = append(logzioClient.bulk, '\n')
	return output.FLB_OK
}

func (logzioClient *LogzioClient) sendBulk() int {
	if len(logzioClient.bulk) == 0 {
		return output.FLB_OK
	}

	req, status := logzioClient.createRequest()
	if status != output.FLB_OK {
		return status
	}

	respCode := logzioClient.doRequest(req)
	if respCode != output.FLB_OK {
		return logzioClient.shouldRetry(respCode)
	}

	return output.FLB_OK
}

func (logzioClient *LogzioClient) createRequest() (*http.Request, int) {
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)

	if _, err := gzipWriter.Write(logzioClient.bulk); err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed to write body with gzip writer: %+v", err))
		return nil, output.FLB_RETRY
	}

	if err := gzipWriter.Close(); err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed to close gzip writer: %+v", err))
		return nil, output.FLB_RETRY
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/?token=%s", logzioClient.listenerURL, logzioClient.token), &buf)
	if err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed to create a request: %+v", err))
		return nil, output.FLB_RETRY
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	return req, output.FLB_OK
}

func (logzioClient *LogzioClient) doRequest(req *http.Request) int {
	resp, err := logzioClient.client.Do(req)
	if err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed to do client request (retryable): %+v", err))
		return output.FLB_RETRY
	}

	defer resp.Body.Close()

	// While we should be able to read the response body, it's not required.  so log but don't return
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed attempting to read from logz.io listener: %+v.  Status %v", err, resp.StatusCode))
	}
	// retry in the same way as the fluent bit http plugin
	if resp.StatusCode >= 500 {
		logzioClient.logger.Log(fmt.Sprintf("received retryable HTTP status code from logz.io listener: %d", resp.StatusCode))
		if body != nil {
			logzioClient.logger.Log(fmt.Sprintf("  (%v)", string(body)))
		}
		return output.FLB_RETRY
	} else if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		logzioClient.logger.Log(fmt.Sprintf("received non-retryable HTTP status code from logz.io listener: %d", resp.StatusCode))
		if body != nil {
			logzioClient.logger.Log(fmt.Sprintf("  (%v)", string(body)))
		}
		return output.FLB_ERROR
	}
	logzioClient.logger.Debug("successfully sent bulk to logz.io\n")
	return output.FLB_OK
}

func (logzioClient *LogzioClient) shouldRetry(code int) int {
	logzioClient.logger.Debug(fmt.Sprintf("response error code: %d", code))
	// follow fluent bit http plugin pattern
	if code >= 500 {
		logzioClient.logger.Log(fmt.Sprintf("retryable response error code: %d", code))
		return output.FLB_RETRY
	}
	return output.FLB_ERROR
}

// Flush sends one last bulk
func (logzioClient *LogzioClient) Flush() int {
	resp := logzioClient.sendBulk()
	logzioClient.bulk = nil
	return resp
}
