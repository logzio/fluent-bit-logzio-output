//go:build linux || darwin || windows
// +build linux darwin windows

package main

import (
	"C"
	"fmt"
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
	plugin Plugin = &bitPlugin{}
	logger        = NewLogger(outputName, true)
)

type LogzioOutput struct {
	plugin            Plugin
	logger            *Logger
	client            *LogzioClient
	ltype             string
	id                string
	dedotEnabled      bool
	dedotNested       bool
	dedotNewSeperator string
	headers           map[string]string
}

var (
	outputs map[string]LogzioOutput
)

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
			logger.Debug(fmt.Sprintf("failed to initialize output configuration: %v", err))
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
	var ret int
	var ts interface{}
	var record map[interface{}]interface{}
	var id string
	if ctx != nil {
		id = output.FLBPluginGetContext(ctx).(string)
	}

	if id == "" {
		id = defaultId
	}

	logger.Debug(fmt.Sprintf("Flushing for id: %s", id))
	dec := plugin.NewDecoder(data, int(length))

	// Iterate Records
	for {
		// Extract Record
		ret, ts, record = plugin.GetRecord(dec)
		if ret != 0 {
			break
		}

		log, err := serializeRecord(ts, C.GoString(tag), record, outputs[id].ltype, id, outputs[id].dedotEnabled, outputs[id].dedotNested, outputs[id].dedotNewSeperator)
		if err != nil {
			continue
		}
		plugin.Send(log, outputs[id].client)
	}

	return plugin.Flush(outputs[id].client)
}

// FLBPluginExit When Fluent Bit will stop using the instance of the plugin,
// it will trigger the exit callback.
//
//export FLBPluginExit
func FLBPluginExit() int {
	plugin.Flush(nil)
	return output.FLB_OK
}

func initConfigParams(ctx unsafe.Pointer) error {
	debug, err := strconv.ParseBool(output.FLBPluginConfigKey(ctx, "logzio_debug"))
	if err != nil {
		debug = false
	}

	outputId := output.FLBPluginConfigKey(ctx, "id")

	if outputs == nil {
		outputs = make(map[string]LogzioOutput)
	}

	if outputId == "" {
		logger.Debug(fmt.Sprintf("using default id: %s", defaultId))
		outputId = defaultId
	}

	if _, ok := outputs[outputId]; ok {
		logger.Log(fmt.Sprintf("outpout_id %s already exists, overriding", outputId))
	}

	logger = NewLogger(outputName+"_"+outputId, debug)
	ltype := output.FLBPluginConfigKey(ctx, "logzio_type")
	if ltype == "" {
		logger.Debug(fmt.Sprintf("using default log type: %s", defaultLogType))
		ltype = defaultLogType
	}

	listenerURL := output.FLBPluginConfigKey(ctx, "logzio_url")
	if listenerURL == "" {
		logger.Debug(fmt.Sprintf("using default listener url: %s", defaultURL))
		listenerURL = defaultURL
	}

	token := output.FLBPluginConfigKey(ctx, "logzio_token")
	if token == "" {
		return fmt.Errorf("token is empty")
	}

	dedotEnabled, err := strconv.ParseBool(output.FLBPluginConfigKey(ctx, "dedot_enabled"))
	dedotNested := false
	dedotNewSeperator := ""
	if err == nil {
		dedotNested, err = strconv.ParseBool(output.FLBPluginConfigKey(ctx, "dedot_nested"))
		if err != nil {
			logger.Debug(fmt.Sprintf("Failed parsing dedot nested value, set to false"))
		}

		dedotNewSeperator = output.FLBPluginConfigKey(ctx, "dedot_new_seperator")
		if dedotNewSeperator == "" || dedotNewSeperator == "." {
			logger.Debug(fmt.Sprintf("Failed parsing dedot new seperator value, set to _"))
			dedotNewSeperator = "_"
		}
	} else {
		logger.Debug(fmt.Sprintf("Failed parsing dedotEnabled value, set to false"))
	}
	logger.Debug(fmt.Sprintf("dedot seperator: %s", dedotNewSeperator))

	proxyHost := output.FLBPluginConfigKey(ctx, "proxy_host") // proxyHost:proxyPort
	proxyUser := output.FLBPluginConfigKey(ctx, "proxy_user") // admin
	proxyPass := output.FLBPluginConfigKey(ctx, "proxy_pass") // password1234

	headers := make(map[string]string)
	headerConfig := output.FLBPluginConfigKey(ctx, "headers")
	if headerConfig != "" {
		for _, header := range strings.Split(headerConfig, ",") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) == 2 {
				key := strings.TrimSpace(parts[0])
				value := strings.TrimSpace(parts[1])
				if key == "" {
					logger.Warn(fmt.Sprintf("Warning: header '%s' has no key. Skipping.", header))
					continue
				}
				if _, exists := headers[key]; exists {
					logger.Warn(fmt.Sprintf("Warning: duplicate header key '%s'. Overwriting existing value.", key))
				}
				headers[key] = value
			} else {
				logger.Warn(fmt.Sprintf("Warning: malformed header '%s'. Expected format 'Key:Value'", header))
			}
		}
	}

	client, err := NewClient(token,
		SetURL(listenerURL),
		SetDebug(debug),
		SetProxy(proxyHost, proxyUser, proxyPass),
		SetHeaders(headers),
	)

	if err != nil {
		return fmt.Errorf("failed to create new client: %+v", err)
	}

	outputs[outputId] = LogzioOutput{
		logger:            logger,
		client:            client,
		ltype:             ltype,
		id:                outputId,
		dedotEnabled:      dedotEnabled,
		dedotNested:       dedotNested,
		dedotNewSeperator: dedotNewSeperator,
		headers:           headers,
	}

	return nil
}

func serializeRecord(ts interface{}, tag string, record map[interface{}]interface{}, ltype string, outputId string, dedotEnabled bool, dedotNested bool, newSeperator string) ([]byte, error) {
	body := parseJSON(record, dedotEnabled, dedotNested, newSeperator)
	var err error
	if _, ok := body["type"]; !ok {
		if ltype != "" {
			body["type"] = ltype
		}
	}

	if _, ok := body["output_id"]; !ok {
		if ltype != "" {
			body["output_id"] = outputId
		}
	}

	if _, ok := body["host"]; !ok {
		// Get hostname
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "localhost"
		}
		body["host"] = hostname
	}

	body["@timestamp"] = formatTimestamp(ts)
	body["fluentbit_tag"] = tag

	serialized, err := jsoniter.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to convert %+v to JSON: %v", record, err)
	}

	return serialized, nil
}

func parseJSON(record map[interface{}]interface{}, dedotEnabled bool, dedotNested bool, dedotNewSeperator string) map[string]interface{} {
	jsonRecord := make(map[string]interface{})

	for k, v := range record {
		stringKey := k.(string)
		if dedotEnabled {
			regex := regexp.MustCompile("\\.")
			stringKey = regex.ReplaceAllString(stringKey, dedotNewSeperator)
		}

		switch t := v.(type) {
		case []byte:
			// prevent encoding to base64
			jsonRecord[stringKey] = string(t)
		case map[interface{}]interface{}:
			if !dedotNested {
				dedotEnabled = false
			}
			jsonRecord[stringKey] = parseJSON(t, dedotEnabled, dedotNested, dedotNewSeperator)
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
					array = append(array, parseJSON(t, dedotEnabled, dedotNested, dedotNewSeperator))
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
