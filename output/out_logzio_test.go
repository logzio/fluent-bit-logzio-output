package main

import (
	"encoding/json"
	"testing"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
	"github.com/stretchr/testify/require"
)

const (
	testType  = "testType"
	testToken = "testToken"
	testURL   = "testUrl"
	testDebug = "true"
)

func TestSerializeRecord(test *testing.T) {
	record := make(map[interface{}]interface{})
	record["key"] = "value"
	record["five"] = 5
	testServer := time.Now()

	serialize, err := serializeRecord(testServer, "atag", record, "logzio", defaultId)
	require.NoError(test, err)
	require.NotNil(test, serialize, "nil json")

	result := make(map[string]interface{})
	err = json.Unmarshal(serialize, &result)
	if err != nil {
		require.NotNil(test, err)
	}

	require.Equal(test, result["fluentbit_tag"], "atag")
	require.Equal(test, result["key"], "value")
	require.Equal(test, result["five"], float64(5))
}

type TestPlugin struct {
	ltype     string
	token     string
	url       string
	debug     string
	logs      []string
	rcounter  int
	output_id string
}

func (p *TestPlugin) Environment(ctx unsafe.Pointer, key string) string {
	switch key {
	case "logzio_type":
		return p.ltype
	case "logzio_token":
		return p.token
	case "logzio_url":
		return p.url
	case "logzio_debug":
		return p.debug
	}
	return "not found"
}

func (p *TestPlugin) Unregister(ctx unsafe.Pointer)                                 {}
func (p *TestPlugin) NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder { return nil }
func (p *TestPlugin) Flush(*LogzioClient) int                                       { return output.FLB_OK }

func (p *TestPlugin) Send(log []byte, client *LogzioClient) int {
	p.logs = append(p.logs, string(log))
	return output.FLB_OK
}

func (p *TestPlugin) GetRecord(dec *output.FLBDecoder) (int, interface{}, map[interface{}]interface{}) {
	if p.rcounter == len(p.logs) {
		return -1, nil, nil
	}

	record := map[interface{}]interface{}{
		"type":      "override",
		"host":      "host",
		"output_id": defaultId,
	}

	foo := map[interface{}]interface{}{
		"bar": "1",
		"baz": 2,
	}

	record["foo"] = foo

	return 0, time.Now(), record
}

func TestPluginInitialization(test *testing.T) {
	plugin = &TestPlugin{
		ltype: testType,
		token: testToken,
		url:   testURL,
		debug: testDebug,
	}
	res := FLBPluginInit(nil)
	require.Equal(test, output.FLB_ERROR, res)
}

func TestPluginFlusher(test *testing.T) {
	tp := &TestPlugin{
		ltype:     testType,
		token:     testToken,
		url:       testURL,
		debug:     testDebug,
		output_id: defaultId,
		rcounter:  1,
	}
	plugin = tp
	res := FLBPluginFlushCtx(nil, nil, 0, nil)
	require.Equal(test, output.FLB_OK, res)

	var j map[string]interface{}
	blog := []byte(tp.logs[0])
	err := json.Unmarshal(blog, &j)
	if err != nil {
		require.NotNil(test, err)
	}

	foo := j["foo"].(map[string]interface{})
	require.Equal(test, foo["bar"], "1")
	require.Equal(test, foo["baz"], float64(2))
	require.Equal(test, j["type"], "override")
	require.Equal(test, j["host"], "host")
	require.Equal(test, j["output_id"], "logzio_output_1")
	require.Contains(test, j, "@timestamp")
}
