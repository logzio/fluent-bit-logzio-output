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

func TestSerializeRecord(t *testing.T) {
	record := make(map[interface{}]interface{})
	record["key"] = "value"
	record["five"] = 5
	ts := time.Now()

	s, err := serializeRecord(ts, "atag", record)
	require.NoError(t, err)
	require.NotNil(t, s, "nil json")

	result := make(map[string]interface{})
	err = json.Unmarshal(s, &result)
	if err != nil {
		require.NotNil(t, err)
	}

	require.Equal(t, result["fluentbit_tag"], "atag")
	require.Equal(t, result["key"], "value")
	require.Equal(t, result["five"], float64(5))
}

type TestPlugin struct {
	ltype string
	token string
	url   string
	debug string

	logs     []string
	rcounter int
}

func (p *TestPlugin) Environment(ctx unsafe.Pointer, key string) string {
	switch key {
	case "Type":
		return p.ltype
	case "Token":
		return p.token
	case "URL":
		return p.url
	case "Debug":
		return p.debug
	}
	return "not found"
}

func (p *TestPlugin) Unregister(ctx unsafe.Pointer)                                 {}
func (p *TestPlugin) NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder { return nil }
func (p *TestPlugin) Flush() int                                                    { return output.FLB_OK }

func (p *TestPlugin) Send(log []byte) int {
	p.logs = append(p.logs, string(log))
	return output.FLB_OK
}

func (p *TestPlugin) GetRecord(dec *output.FLBDecoder) (int, interface{}, map[interface{}]interface{}) {
	if p.rcounter == len(p.logs) {
		return -1, nil, nil
	}

	record := map[interface{}]interface{}{
		"type": "override",
		"host": "host",
	}

	foo := map[interface{}]interface{}{
		"bar": "1",
		"baz": 2,
	}

	record["foo"] = foo

	return 0, time.Now(), record
}

func TestPluginInitialization(t *testing.T) {
	plugin = &TestPlugin{
		ltype: testType,
		token: testToken,
		url:   testURL,
		debug: testDebug,
	}
	res := FLBPluginInit(nil)
	require.Equal(t, output.FLB_OK, res)
}

func TestPluginMissingToken(t *testing.T) {
	plugin = &TestPlugin{
		ltype: testType,
		url:   testURL,
		debug: testDebug,
	}
	res := FLBPluginInit(nil)
	require.Equal(t, output.FLB_ERROR, res)
}

func TestPluginMissingURL(t *testing.T) {
	plugin = &TestPlugin{
		ltype: testType,
		token: testToken,
		debug: testDebug,
	}
	res := FLBPluginInit(nil)
	require.Equal(t, output.FLB_ERROR, res)
}

func TestPluginFlusher(t *testing.T) {
	tp := &TestPlugin{
		ltype:    testType,
		token:    testToken,
		url:      testURL,
		debug:    testDebug,
		rcounter: 1,
	}
	plugin = tp
	res := FLBPluginFlush(nil, 0, nil)
	require.Equal(t, output.FLB_OK, res)

	var j map[string]interface{}
	blog := []byte(tp.logs[0])
	err := json.Unmarshal(blog, &j)
	if err != nil {
		require.NotNil(t, err)
	}

	foo := j["foo"].(map[string]interface{})
	require.Equal(t, foo["bar"], "1")
	require.Equal(t, foo["baz"], float64(2))
	require.Equal(t, j["type"], "override")
	require.Equal(t, j["host"], "host")
	require.Contains(t, j, "@timestamp")
}
