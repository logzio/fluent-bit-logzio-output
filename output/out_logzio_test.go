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
	testType            = "testType"
	testToken           = "testToken"
	testURL             = "testUrl"
	testDebug           = "true"
	testId              = "testOutputId"
	testBulkSizeValid   = "5"
	testBulkSizeInvalid = "abc"
	testBulkSizeTooLow  = "0"
	testBulkSizeTooHigh = "10"
)

type TestPluginMock struct {
	config     map[string]string
	sentLogs   [][]byte
	records    []map[interface{}]interface{}
	recCounter int
}

func (p *TestPluginMock) Environment(ctx unsafe.Pointer, key string) string {
	if val, ok := p.config[key]; ok {
		return val
	}
	return ""
}
func (p *TestPluginMock) Unregister(ctx unsafe.Pointer) {}
func (p *TestPluginMock) NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder { return nil }
func (p *TestPluginMock) Flush(client *LogzioClient) int {
	return output.FLB_OK
}
func (p *TestPluginMock) Send(logBytes []byte, client *LogzioClient) int {
	p.sentLogs = append(p.sentLogs, logBytes)
	return output.FLB_OK
}
func (p *TestPluginMock) GetRecord(dec *output.FLBDecoder) (int, interface{}, map[interface{}]interface{}) {
	if p.recCounter >= len(p.records) {
		return -1, nil, nil
	}
	record := p.records[p.recCounter]
	p.recCounter++
	return 0, output.FLBTime{Time: time.Now()}, record
}
func NewTestPluginMock(config map[string]string, records []map[interface{}]interface{}) *TestPluginMock {
	return &TestPluginMock{config: config, records: records}
}

// --- Test Cases ---

func TestSerializeRecord(test *testing.T) {
	instanceLogger := NewLogger("testSerialize", true)
	testInstance := &LogzioOutput{
		logger:            instanceLogger,
		ltype:             "type1",
		id:                "out1",
		dedotEnabled:      false,
		dedotNewSeperator: "_",
	}
	record := map[interface{}]interface{}{"key": "value"}
	testTime := time.Now()
	serializedNoDedot, err := serializeRecord(testTime, "tag1", record, testInstance)
	require.NoError(test, err)
	resultNoDedot := make(map[string]interface{})
	err = json.Unmarshal(serializedNoDedot, &resultNoDedot)
	require.NoError(test, err)
	require.Equal(test, "value", resultNoDedot["key"])
	require.Equal(test, "tag1", resultNoDedot["fluentbit_tag"])
}

func TestPluginInitializationBasic(test *testing.T) {
	mockMissingToken := NewTestPluginMock(map[string]string{"id": testId}, nil)
	plugin = mockMissingToken
	err := initConfigParams(unsafe.Pointer(uintptr(0)))
	require.Error(test, err)
	require.EqualError(test, err, "required parameter 'logzio_token' is missing")

	mockValid := NewTestPluginMock(map[string]string{"logzio_token": testToken, "id": testId}, nil)
	plugin = mockValid
	outputs = nil
	err = initConfigParams(unsafe.Pointer(uintptr(0)))
	require.NoError(test, err)
	require.NotNil(test, outputs)
	instance, ok := outputs[testId]
	require.True(test, ok)
	require.NotNil(test, instance)
	require.NotNil(test, instance.client)
	require.Equal(test, defaultSizeThresholdMB*megaByte, instance.client.sizeThresholdInBytes)
}

func TestPluginInitializationBulkSize(test *testing.T) {
	mockValidSize := NewTestPluginMock(map[string]string{
		"logzio_token":        testToken,
		"id":                  testId,
		"logzio_bulk_size_mb": testBulkSizeValid,
	}, nil)
	plugin = mockValidSize
	outputs = nil
	err := initConfigParams(unsafe.Pointer(uintptr(0)))
	require.NoError(test, err)
	require.NotNil(test, outputs[testId])
	require.Equal(test, 5*megaByte, outputs[testId].client.sizeThresholdInBytes)

	mockInvalidSize := NewTestPluginMock(map[string]string{
		"logzio_token":        testToken,
		"id":                  testId,
		"logzio_bulk_size_mb": testBulkSizeInvalid,
	}, nil)
	plugin = mockInvalidSize
	outputs = nil
	err = initConfigParams(unsafe.Pointer(uintptr(0)))
	require.NoError(test, err)
	require.NotNil(test, outputs[testId])
	require.Equal(test, defaultSizeThresholdMB*megaByte, outputs[testId].client.sizeThresholdInBytes)

	mockTooLowSize := NewTestPluginMock(map[string]string{
		"logzio_token":        testToken,
		"id":                  testId,
		"logzio_bulk_size_mb": testBulkSizeTooLow,
	}, nil)
	plugin = mockTooLowSize
	outputs = nil
	err = initConfigParams(unsafe.Pointer(uintptr(0)))
	require.NoError(test, err)
	require.NotNil(test, outputs[testId])
	require.Equal(test, defaultSizeThresholdMB*megaByte, outputs[testId].client.sizeThresholdInBytes)

	mockTooHighSize := NewTestPluginMock(map[string]string{
		"logzio_token":        testToken,
		"id":                  testId,
		"logzio_bulk_size_mb": testBulkSizeTooHigh,
	}, nil)
	plugin = mockTooHighSize
	outputs = nil
	err = initConfigParams(unsafe.Pointer(uintptr(0)))
	require.NoError(test, err)
	require.NotNil(test, outputs[testId])
	require.Equal(test, defaultSizeThresholdMB*megaByte, outputs[testId].client.sizeThresholdInBytes)
}

func TestPluginFlusherMock(test *testing.T) {
	// 1. Define mock records and config
	records := []map[interface{}]interface{}{
		{"message": "log 1", "field": "value1"},
		{"message": "log 2", "field": "value2"},
	}
	mockConfig := map[string]string{
		"logzio_token": testToken,
		"id":           testId,
		"logzio_type":  testType,
	}
	mockPlugin := NewTestPluginMock(mockConfig, records)
	plugin = mockPlugin 

	// 2. Initialize the configuration (this populates the global 'outputs' map)
	outputs = make(map[string]*LogzioOutput) 
	err := initConfigParams(unsafe.Pointer(uintptr(0)))
	require.NoError(test, err)
	require.Contains(test, outputs, testId) 
	outputInstance := outputs[testId]  

	// 3. Simulate the core logic of FLBPluginFlushCtx: Iterate records -> Serialize -> Send
	tag := "test.tag"
	for {
		ret, ts, record := mockPlugin.GetRecord(nil) 
		if ret != 0 {
			break 
		}
		logBytes, err := serializeRecord(ts, tag, record, outputInstance)
		require.NoError(test, err)   
		res := plugin.Send(logBytes, outputInstance.client) 
		require.Equal(test, output.FLB_OK, res) 
	}

	// 4. Simulate the final flush call
	res := plugin.Flush(outputInstance.client) 
	require.Equal(test, output.FLB_OK, res)

	// 5. Verify results: Check that the mock's Send method was called correctly
	require.Len(test, mockPlugin.sentLogs, len(records)) 

	var log1Data map[string]interface{}
	err = json.Unmarshal(mockPlugin.sentLogs[0], &log1Data)
	require.NoError(test, err)
	require.Equal(test, "log 1", log1Data["message"])
	require.Equal(test, testType, log1Data["type"])
	require.Equal(test, testId, log1Data["output_id"])
}