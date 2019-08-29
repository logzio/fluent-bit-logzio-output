package main

import (
	"C"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
	"unsafe"

	"github.com/fluent/fluent-bit-go/output"
)

const (
	outputDescription = "This is fluent-bit output plugin that sends data to Logz.io"
	outputName        = "logzio"
)

// Initialize output parameters
var (
	plugin Plugin = &bitPlugin{}

	logger *Logger
	client *LogzioClient
	ltype  string
)

// Plugin interface
type Plugin interface {
	Environment(ctx unsafe.Pointer, key string) string
	Unregister(ctx unsafe.Pointer)
	GetRecord(dec *output.FLBDecoder) (ret int, ts interface{}, rec map[interface{}]interface{})
	NewDecoder(data unsafe.Pointer, length int) *output.FLBDecoder
	Send(values []byte) int
	Flush() int
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

func (p *bitPlugin) Send(log []byte) int {
	return client.Send(log)
}

func (p *bitPlugin) Flush() int {
	return client.Flush()
}

// FLBPluginRegister When Fluent Bit loads a Golang plugin,
// it looks up and loads the registration callback that aims
// to populate the internal structure with plugin name and description.
// This function is invoked at start time before any configuration is done inside the engine.
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
//export FLBPluginInit
func FLBPluginInit(ctx unsafe.Pointer) int {
	if err := initConfigParams(ctx); err != nil {
		logger.Log(fmt.Sprintf("failed to initialize output configuration: %v", err))
		plugin.Unregister(ctx)
		return output.FLB_ERROR
	}
	return output.FLB_OK
}

// FLBPluginFlush Upon flush time, when Fluent Bit wants to flush it's buffers,
// the runtime flush callback will be triggered.
// The callback will receive the configuration context,
// a raw buffer of msgpack data,
// the proper bytes length and the associated tag.
// When done, there are three returning values available: FLB_OK, FLB_ERROR, FLB_RETRY.
//export FLBPluginFlush
func FLBPluginFlush(data unsafe.Pointer, length C.int, tag *C.char) int {
	var ret int
	var ts interface{}
	var record map[interface{}]interface{}

	// Create Fluent-Bit decoder
	dec := plugin.NewDecoder(data, int(length))

	// Iterate Records
	for {
		// Extract Record
		ret, ts, record = plugin.GetRecord(dec)
		if ret != 0 {
			break
		}

		log, err := serializeRecord(ts, C.GoString(tag), record)
		if err != nil {
			continue
		}
		plugin.Send(log)
	}

	return plugin.Flush()
}

//FLBPluginExit When Fluent Bit will stop using the instance of the plugin,
// it will trigger the exit callback.
//export FLBPluginExit
func FLBPluginExit() int {
	return output.FLB_OK
}

func initConfigParams(ctx unsafe.Pointer) error {
	b, err := strconv.ParseBool(plugin.Environment(ctx, "Debug"))
	if err != nil {
		logger.Debug("using default debug = false")
		b = false
	}

	logger = NewLogger(outputName, b)
	logger.Debug("initializing output plugin..")

	ltype = plugin.Environment(ctx, "Type")

	url := plugin.Environment(ctx, "URL")
	if url == "" {
		return fmt.Errorf("URL is empty")
	}

	token := plugin.Environment(ctx, "Token")
	if token == "" {
		return fmt.Errorf("token is empty")
	}

	client, err = NewClient(token,
		SetURL(url),
	)

	if err != nil {
		return fmt.Errorf("failed to create new client: %+v", err)
	}

	return nil
}

func serializeRecord(ts interface{}, tag string, record map[interface{}]interface{}) ([]byte, error) {
	body := parseJSON(record)
	if _, ok := body["type"]; !ok {
		if ltype != "" {
			body["type"] = ltype
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

	serialized, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to convert %+v to JSON: %v", record, err)
	}

	return serialized, nil
}

func parseJSON(record map[interface{}]interface{}) map[string]interface{} {
	m := make(map[string]interface{})
	for k, v := range record {
		switch t := v.(type) {
		case []byte:
			// prevent encoding to base64
			m[k.(string)] = string(t)
		case map[interface{}]interface{}:
			m[k.(string)] = parseJSON(t)
		default:
			m[k.(string)] = v
		}
	}
	return m
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
	default:
		fmt.Print("Unknown format, defaulting to now.\n")
		timestamp = time.Now()
	}
	return timestamp
}

func main() {
}
