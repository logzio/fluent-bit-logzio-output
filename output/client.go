package main

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/fluent/fluent-bit-go/output"
)

const (
	defaultURL         = "https://listener.logz.io:8071"
	maxRequestBodySize = 9 * 1024 * 1024 // 9MB
)

// LogzioClient http client that sends bulks to Logz.io http listener
type LogzioClient struct {
	url   string
	token string

	bulk      []byte
	client    *http.Client
	logger    *Logger
	threshold int
}

// ClientOptionFunc options for Logz.io
type ClientOptionFunc func(*LogzioClient) error

// NewClient is a constructor for Logz.io http client
func NewClient(token string, options ...ClientOptionFunc) (*LogzioClient, error) {
	lc := &LogzioClient{
		url:       defaultURL,
		token:     token,
		logger:    NewLogger(outputName, false),
		threshold: maxRequestBodySize,
	}

	tlsConfig := &tls.Config{}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		Proxy:           http.ProxyFromEnvironment,
	}
	// in case server side is sleeping - wait 10s instead of waiting for him to wake up
	c := &http.Client{
		Transport: transport,
		Timeout:   time.Second * 10,
	}

	lc.client = c

	for _, option := range options {
		if err := option(lc); err != nil {
			return nil, err
		}
	}

	return lc, nil
}

// SetURL set the url which maybe different from the defaultUrl
func SetURL(url string) ClientOptionFunc {
	return func(lc *LogzioClient) error {
		lc.url = url
		lc.logger.Debug(fmt.Sprintf("setting url to %s\n", url))
		return nil
	}
}

// SetDebug mode and send logs to this writer
func SetDebug(debug bool) ClientOptionFunc {
	return func(lc *LogzioClient) error {
		lc.logger.SetDebug(debug)
		lc.logger.Debug(fmt.Sprintf("setting debug to %t\n", debug))
		return nil
	}
}

// SetBodySizeThreshold set the maximum body size of the client http request
// The param in in MB and can be between 0(mostly for testing) and 9
func SetBodySizeThreshold(t int) ClientOptionFunc {
	return func(lc *LogzioClient) error {
		lc.threshold = t
		if t < 0 || t > 9 {
			lc.logger.Debug("falling back to the default BodySizeThreshold")
			lc.threshold = maxRequestBodySize
		}
		lc.logger.Debug(fmt.Sprintf("setting BodySizeThreshold to %d\n", lc.threshold))
		return nil
	}
}

// Send adds the log to the client bulk slice check if we should send the bulk
func (lc *LogzioClient) Send(log []byte) int {
	// Logz.io maximum request body size is 10MB. We send bulks that
	// exceed this size (with a safety buffer) via separate write requests.
	if (len(lc.bulk) + len(log) + 1) > lc.threshold {
		res := lc.sendBulk()
		lc.bulk = nil
		if res != output.FLB_OK {
			return res
		}
	}
	lc.logger.Debug(fmt.Sprintf("adding log to the bulk: %+v\n", string(log)))
	lc.bulk = append(lc.bulk, log...)
	lc.bulk = append(lc.bulk, '\n')
	return output.FLB_OK
}

func (lc *LogzioClient) sendBulk() int {
	if len(lc.bulk) == 0 {
		return output.FLB_OK
	}

	req, status := lc.createRequest()
	if status != output.FLB_OK {
		return status
	}

	respCode, ok := lc.doRequest(req)
	if !ok {
		return lc.shouldRetry(respCode)
	}

	return output.FLB_OK
}

func (lc *LogzioClient) createRequest() (*http.Request, int) {
	var buf bytes.Buffer
	g := gzip.NewWriter(&buf)

	if _, err := g.Write(lc.bulk); err != nil {
		lc.logger.Log(fmt.Sprintf("failed to write body with gzip writer: %+v", err))
		return nil, output.FLB_ERROR
	}

	if err := g.Close(); err != nil {
		lc.logger.Log(fmt.Sprintf("failed to close gzip writer: %+v", err))
		return nil, output.FLB_ERROR
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/?token=%s", lc.url, lc.token), &buf)
	if err != nil {
		lc.logger.Log(fmt.Sprintf("failed to create a request: %+v", err))
		return nil, output.FLB_ERROR
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	return req, output.FLB_OK
}

func (lc *LogzioClient) doRequest(req *http.Request) (int, bool) {
	resp, err := lc.client.Do(req)
	if err != nil {
		lc.logger.Log(fmt.Sprintf("failed to do client request: %+v", err))
		return output.FLB_ERROR, false
	}
	defer resp.Body.Close()

	_, err = ioutil.ReadAll(resp.Body)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode > 299 {
		lc.logger.Log(fmt.Sprintf("recieved an error from logz.io listener: %+v", err))
		return resp.StatusCode, false
	}
	lc.logger.Debug("successfully sent bulk to logz.io\n")
	return output.FLB_OK, true
}

func (lc *LogzioClient) shouldRetry(code int) int {
	lc.logger.Debug(fmt.Sprintf("response error code: %d", code))
	if code >= 500 {
		return output.FLB_RETRY
	}
	return output.FLB_ERROR
}

// Flush sends one last bulk
func (lc *LogzioClient) Flush() int {
	resp := lc.sendBulk()
	lc.bulk = nil
	return resp
}
