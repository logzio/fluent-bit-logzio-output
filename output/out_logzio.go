//go:build linux || darwin || windows
// +build linux darwin windows

package main

import (
	"C"
	"fmt"
	"log"
	"github.com/fluent/fluent-bit-go/output"
	jsoniter "github.com/json-iterator/go"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

const (
	outputDescription = "This is a fluent-bit output plugin that sends data to Logz.io"
	outputName        = "logzio"
	defaultLogType    = "logzio-fluent-bit"
)

var (
	outputs map[string]*LogzioOutput
	plugin  Plugin = &bitPlugin{}
)

type LogzioOutput struct {
	logger            *Logger
	client            *LogzioClient
	ltype             string
	id                string
	dedotEnabled      bool
	dedotNested       bool
	dedotNewSeparator string
	headers           map[string]string
}

// Plugin interface
type Plugin interface {
	Environment(ctx unsafe.Pointer, key string) string
	Unregister(ctx unsafe.Pointer)
	GetRecord(dec *output.FLBDecoder) (ret int, ts interface{}, rec map[interface{}]interface{})
	NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder
	Send(values []byte, client *LogzioClient) int
	Flush(*LogzioClient) int
}

type bitPlugin struct{}

func (p *bitPlugin) Environment(ctx unsafe.Pointer, key string) string {
	return output.FLBPluginConfigKey(ctx, key)
}

func (p *bitPlugin) Unregister(ctx unsafe.Pointer) {
	output.FLBPluginUnregister(ctx)
}

func (p *bitPlugin) GetRecord(dec *output.FLBDecoder) (ret int, ts interface{}, rec map[interface{}]interface{}) {
	return output.GetRecord(dec)
}

func (p *bitPlugin) NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder {
	return output.NewDecoder(data, length)
}

func (p *bitPlugin) Send(log []byte, client *LogzioClient) int {
	return client.Send(log)
}

func (p *bitPlugin) Flush(client *LogzioClient) int {
	return client.Flush()
}

// FLBPluginRegister When Fluent Bit loads a Golang plugin,
// it looks up and loads the registration callback that aims
// to populate the internal structure with plugin name and description.
// This function is invoked at start time before any configuration is done inside the engine.
//
//export FLBPluginRegister
func FLBPluginRegister(ctx unsafe.Pointer) int {
	return output.FLBPluginRegister(ctx, outputName, outputDescription)
}

// FLBPluginInit Before the engine starts,
// it initializes all plugins that were configured.
// As part of the initialization, the plugin can obtain configuration parameters and do any other internal checks.
// It can also set the context for this instance in case params need to be retrieved during flush.
// The function must return FLB_OK when it initialized properly or FLB_ERROR if something went wrong.
// If the plugin reports an error, the engine will not load the instance.
//
//export FLBPluginInit
func FLBPluginInit(ctx unsafe.Pointer) int {
	if ctx != nil {
		if err := initConfigParams(ctx); err != nil {
			log.Printf("[%s] failed to initialize output configuration: %v", outputName, err)
			plugin.Unregister(ctx)
			return output.FLB_ERROR
		}

		output.FLBPluginSetContext(ctx, output.FLBPluginConfigKey(ctx, "id"))
	} else {
		return output.FLB_ERROR
	}
	return output.FLB_OK
}

//export FLBPluginFlush
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	return output.FLB_OK
}

// FLBPluginFlush Upon flush time, when Fluent Bit wants to flush it's buffers,
// the runtime flush callback will be triggered.
// The callback will receive the configuration context,
// a raw buffer of msgpack data,
// the proper bytes length and the associated tag.
// When done, there are three returning values available: FLB_OK, FLB_ERROR, FLB_RETRY.
//
//export FLBPluginFlushCtx
func FLBPluginFlushCtx(ctx, data unsafe.Pointer, length C.int, tag *C.char) int {
	var id string
	var outputInstance *LogzioOutput
	var ok bool

	// Get ID from context
	ctxID := output.FLBPluginGetContext(ctx)
	if ctxID == nil {
		log.Printf("[%s] Error: Flush context is nil.", outputName)
		return output.FLB_ERROR
	}
	id, ok = ctxID.(string)
	if !ok || id == "" {
		log.Printf("[%s] Error: Invalid context ID (%v).", outputName, ctxID)
		return output.FLB_ERROR
	}

	// Retrieve instance config
	outputInstance, ok = outputs[id]
	if !ok {
		log.Printf("[%s] Error: Config missing for output ID '%s'.", outputName, id)
		return output.FLB_ERROR
	}
	instanceLogger := outputInstance.logger

	dec := plugin.NewDecoder(data, int(length))

	lastErrCode := output.FLB_OK
	for {
		ret, ts, record := plugin.GetRecord(dec)
		if ret != 0 {
			break
		}

		// Pass instance to serializeRecord
		logBytes, err := serializeRecord(ts, C.GoString(tag), record, outputInstance)
		if err != nil {
			instanceLogger.Log(fmt.Sprintf("Error serializing record: %v. Skipping.", err))
			continue
		}

		res := plugin.Send(logBytes, outputInstance.client)
		if res != output.FLB_OK {
			instanceLogger.Log(fmt.Sprintf("Send returned error code %d.", res))
			lastErrCode = res
		}
	}

	// Final Flush
	flushResult := plugin.Flush(outputInstance.client)
	if flushResult != output.FLB_OK {
		instanceLogger.Log(fmt.Sprintf("Final Flush returned error code %d.", flushResult))
		if lastErrCode == output.FLB_OK || flushResult == output.FLB_ERROR {
			lastErrCode = flushResult
		}
	}

	return lastErrCode
}

// FLBPluginExit When Fluent Bit will stop using the instance of the plugin,
// it will trigger the exit callback.
//
//export FLBPluginExit
func FLBPluginExit() int {
	for _, exporter := range outputs {
		plugin.Flush(exporter.client)
	}
	return output.FLB_OK
}

func initConfigParams(ctx unsafe.Pointer) error {
	outputId := plugin.Environment(ctx, "id")
	if outputId == "" {
		outputId = "logzio_output_1"
	}

	// Basic Config & Logger setup
	debugStr := plugin.Environment(ctx, "logzio_debug")
	debug, _ := strconv.ParseBool(debugStr) 
	instanceLogger := NewLogger(fmt.Sprintf("%s_%s", outputName, outputId), debug)

	if outputs == nil {
		outputs = make(map[string]*LogzioOutput)
	}
	// Check for duplicate ID warning
	if _, exists := outputs[outputId]; exists {
		instanceLogger.Warn(fmt.Sprintf("Output instance with id '%s' already configured. Overwriting.", outputId))
	}

	// Read other parameters
	ltype := plugin.Environment(ctx, "logzio_type")
	if ltype == "" {
		instanceLogger.Debug(fmt.Sprintf("logzio_type not set, using default: %s", defaultLogType))
		ltype = defaultLogType
	}
	listenerURL := plugin.Environment(ctx, "logzio_url")
	if listenerURL == "" {
		instanceLogger.Debug(fmt.Sprintf("logzio_url not set, using default: %s", defaultURL)) 
		listenerURL = defaultURL
	}
	token := plugin.Environment(ctx, "logzio_token")
	if token == "" {
		return fmt.Errorf("required parameter 'logzio_token' is missing")
	}

	// Dedot Config
	dedotEnabledStr := plugin.Environment(ctx, "dedot_enabled")
	dedotEnabled, err := strconv.ParseBool(dedotEnabledStr) 
	dedotNested := false
	dedotNewSeparator := "_" 
	if err == nil && dedotEnabled {
		dedotNestedStr := plugin.Environment(ctx, "dedot_nested")
		dedotNested, err = strconv.ParseBool(dedotNestedStr)
		if err != nil {
			instanceLogger.Debug(fmt.Sprintf("Failed parsing dedot nested value, set to false"))
			dedotNested = false 
		}
		dedotNewSeparator = plugin.Environment(ctx, "dedot_new_separator")
		if dedotNewSeparator == "" || dedotNewSeparator == "." {
			instanceLogger.Debug(fmt.Sprintf("Invalid or empty dedot new separator value, set to _"))
			dedotNewSeparator = "_"
		}
	} else {
		instanceLogger.Debug(fmt.Sprintf("dedot_enabled is false or failed to parse, disabling dedot features"))
		dedotEnabled = false // Ensure false
	}
	instanceLogger.Debug(fmt.Sprintf("Dedot enabled: %t, Nested: %t, Separator: '%s'", dedotEnabled, dedotNested, dedotNewSeparator))

	// Proxy Config
	proxyHost := plugin.Environment(ctx, "proxy_host")
	proxyUser := plugin.Environment(ctx, "proxy_user")
	proxyPass := plugin.Environment(ctx, "proxy_pass")

	// Headers Config
	headers := make(map[string]string)
	headerConfig := plugin.Environment(ctx, "headers")
	if headerConfig != "" {
		for _, header := range strings.Split(headerConfig, ",") {
			parts := strings.SplitN(strings.TrimSpace(header), ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				if key == "" {
					instanceLogger.Warn(fmt.Sprintf("Warning: header '%s' has no key. Skipping.", header))
					continue
				}
				headers[key] = value
			} else if strings.TrimSpace(header) != "" {
				instanceLogger.Warn(fmt.Sprintf("Warning: malformed header '%s'. Expected format 'Key:Value'", header))
			}
		}
	}

	// Bulk Size Config
	bulkSizeMBStr := plugin.Environment(ctx, "logzio_bulk_size_mb")
	var bulkSizeOption ClientOptionFunc
	if bulkSizeMBStr != "" {
		bulkSizeMB, err := strconv.Atoi(bulkSizeMBStr)
		if err != nil {
			instanceLogger.Warn(fmt.Sprintf("Failed to parse logzio_bulk_size_mb ('%s'): %v. Using client default.", bulkSizeMBStr, err))
		} else {
			bulkSizeOption = SetBodySizeThresholdMB(bulkSizeMB)
		}
	} else {
	    instanceLogger.Debug("logzio_bulk_size_mb not set. Using client default.")
	}


	// Create Client
	clientOptions := []ClientOptionFunc{
		SetURL(listenerURL),
		SetDebug(debug), 
		SetProxy(proxyHost, proxyUser, proxyPass),
		SetHeaders(headers),
	}
	if bulkSizeOption != nil {
		clientOptions = append(clientOptions, bulkSizeOption)
	}

	client, err := NewClient(token, clientOptions...)
	if err != nil {
		return fmt.Errorf("failed to create LogzioClient: %w", err)
	}
	client.logger = instanceLogger

	outputs[outputId] = &LogzioOutput{
		logger:            instanceLogger,
		client:            client,
		ltype:             ltype,
		id:                outputId,
		dedotEnabled:      dedotEnabled,
		dedotNested:       dedotNested,
		dedotNewSeparator: dedotNewSeparator,
		headers:           headers,
	}

	instanceLogger.Debug("Initialization successful.")
	return nil
}

func serializeRecord(ts interface{}, tag string, record map[interface{}]interface{}, instance *LogzioOutput) ([]byte, error) {
	body := parseJSON(record, instance.dedotEnabled, instance.dedotNested, instance.dedotNewSeparator)

	body["@timestamp"] = formatTimestamp(ts) 
	body["fluentbit_tag"] = tag

	if _, ok := body["type"]; !ok {
		body["type"] = instance.ltype 
	}
	body["output_id"] = instance.id

	if _, ok := body["host"]; !ok {
		hostname, err := os.Hostname()
		if err != nil {
			instance.logger.Warn(fmt.Sprintf("Could not get hostname: %v. Using 'unknown'.", err))
			hostname = "unknown_host"
		}
		body["host"] = hostname
	}

	serialized, err := jsoniter.Marshal(body)
	if err != nil {
		instance.logger.Log(fmt.Sprintf("Failed to marshal record map to JSON: %v", err))
		return nil, fmt.Errorf("failed marshal record: %w", err) // Wrap error
	}

	return serialized, nil
}

func parseJSON(record map[interface{}]interface{}, dedotEnabled bool, dedotNested bool, dedotNewSeparator string) map[string]interface{} {
	jsonRecord := make(map[string]interface{})

	for k, v := range record {
		stringKey := k.(string)
		if dedotEnabled {
			regex := regexp.MustCompile("\\.")
			stringKey = regex.ReplaceAllString(stringKey, dedotNewSeparator)
		}

		switch t := v.(type) {
		case []byte:
			// prevent encoding to base64
			jsonRecord[stringKey] = string(t)
		case map[interface{}]interface{}:
			if !dedotNested {
				dedotEnabled = false
			}
			jsonRecord[stringKey] = parseJSON(t, dedotEnabled, dedotNested, dedotNewSeparator)
		case []interface{}:
			var array []interface{}
			for _, e := range v.([]interface{}) {
				switch t := e.(type) {
				case []byte:
					array = append(array, string(t))
				case map[interface{}]interface{}:
					if !dedotNested {
						dedotEnabled = false
					}
					array = append(array, parseJSON(t, dedotEnabled, dedotNested, dedotNewSeparator))
				default:
					array = append(array, e)
				}
			}
			jsonRecord[stringKey] = array
		default:
			jsonRecord[stringKey] = v
		}
	}
	return jsonRecord
}
func formatTimestamp(ts interface{}) time.Time {
	var timestamp time.Time

	switch t := ts.(type) {
	case output.FLBTime:
		timestamp = ts.(output.FLBTime).Time
	case uint64:
		timestamp = time.Unix(int64(t), 0)
	case time.Time:
		timestamp = ts.(time.Time)
	case []interface{}:
		s := reflect.ValueOf(t)
		if s.Kind() != reflect.Slice || s.Len() < 2 {
			// Expects a non-empty slice of length 2, so we won't extract a timestamp.
			timestamp = formatTimestamp(s)
			return timestamp
		}
		ts = s.Index(0).Interface() // First item is the timestamp.
		timestamp = formatTimestamp(ts)
	default:
		fmt.Printf("Unknown format, defaulting to now, timestamp: %v of type: %T.\n", t, t)
		timestamp = time.Now()
	}
	return timestamp
}

func main() {
}
