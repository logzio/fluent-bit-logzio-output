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
	megaByte                  = 1 * 1024 * 1024 // 1MB
	defaultSizeThresholdMB 	  = 2 
	minSizeThresholdMB     	  = 1 
	maxSizeThresholdMB     	  = 9 

)

// LogzioClient http client that sends bulks to Logz.io http listener
type LogzioClient struct {
	listenerURL          string
	token                string
	bulk                 []byte
	client               *http.Client
	logger               *Logger
	sizeThresholdInBytes int
	headers              map[string]string
}

// ClientOptionFunc options for Logz.io
type ClientOptionFunc func(*LogzioClient) error

// NewClient is a constructor for Logz.io http client
func NewClient(token string, options ...ClientOptionFunc) (*LogzioClient, error) {
	logzioClient := &LogzioClient{
		listenerURL:          defaultURL,
		token:                token,
		logger:               NewLogger(outputName, false),
		sizeThresholdInBytes: defaultSizeThresholdMB * megaByte,
		headers:              make(map[string]string),
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

	logzioClient.logger.Debug(fmt.Sprintf("LogzioClient created. Using bulk size threshold: %d bytes (%d MB)",
		logzioClient.sizeThresholdInBytes, logzioClient.sizeThresholdInBytes/megaByte))

	return logzioClient, nil
}

func SetHeaders(headers map[string]string) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		logzioClient.headers = headers
		return nil
	}
}

// SetURL set the url which maybe different from the defaultUrl
func SetURL(listenerURL string) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		if listenerURL == "" {
			logzioClient.logger.Warn("SetURL called with empty URL, keeping default.")
			return nil
		}
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
func SetBodySizeThresholdMB(thresholdMB int) ClientOptionFunc {
	return func(logzioClient *LogzioClient) error {
		if thresholdMB < minSizeThresholdMB || thresholdMB > maxSizeThresholdMB {
			logzioClient.logger.Warn(fmt.Sprintf("Invalid logzio_bulk_size_mb value (%d). Must be between %d and %d MB. Using default: %d MB.",
				thresholdMB, minSizeThresholdMB, maxSizeThresholdMB, defaultSizeThresholdMB))
			logzioClient.sizeThresholdInBytes = defaultSizeThresholdMB * megaByte
		} else {
			logzioClient.sizeThresholdInBytes = thresholdMB * megaByte
			logzioClient.logger.Debug(fmt.Sprintf("setting BodySizeThresholdMB to %d MB (%d bytes)",
				thresholdMB, logzioClient.sizeThresholdInBytes))
		}
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

	for key, value := range logzioClient.headers {
		req.Header.Set(key, value)
	}

	return req, output.FLB_OK
}

func (logzioClient *LogzioClient) doRequest(req *http.Request) int {
	resp, err := logzioClient.client.Do(req)
	if err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed to do retryable client request: %+v", err))
		return output.FLB_RETRY
	}
	defer resp.Body.Close()

	// While we should be able to read the response body, it's not required.  so log but don't return
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logzioClient.logger.Log(fmt.Sprintf("failed attempting to read from logz.io listener: %+v.  Status %v", err, resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		logzioClient.logger.Log(fmt.Sprintf("received a non-2xx HTTP status code from logz.io listener: %d (%v)", resp.StatusCode, string(body)))
		return resp.StatusCode
	}
	logzioClient.logger.Debug("successfully sent bulk to logz.io\n")
	return output.FLB_OK
}

func (logzioClient *LogzioClient) shouldRetry(code int) int {
	// follow fluent bit http plugin pattern
	if code >= 500 || code == output.FLB_RETRY {
		logzioClient.logger.Debug(fmt.Sprintf("retryable response error code: %d", code))
		return output.FLB_RETRY
	}
	logzioClient.logger.Debug(fmt.Sprintf("non-retryable response error code: %d", code))
	return output.FLB_ERROR
}

// Flush sends one last bulk
func (logzioClient *LogzioClient) Flush() int {
	resp := logzioClient.sendBulk()
	logzioClient.bulk = nil
	return resp
}
