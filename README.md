# fluent-bit-logzio-output
This is a Logz.io fluent-bit output plugin. 

###Prerequisites
* Fluent-Bit installed. For more information on how to install Fluent-Bit: [installation docs](https://docs.fluentbit.io/manual/installation).
* Logz.io output plugin: 

```text
wget -o /fluent-bit/plugins/out_logzio.so https://github.com/logzio/fluent-bit-logzio-output/build/out_logzio.so
```

###Configuration

Add to your Fluent-Bit configuration file Logz.io as an output:
```toml
[OUTPUT]
    Name  logzio
    Match *
    logzio_token <YOUR_LOGZIO_SHIPPING_TOKEN>
    logzio_url   <YOUR_LOGZIO_LISTENER_URL>
    logzio_type  testBit
```

_**Parameters table(logzio_token, logzio_url, logzio_type, logzio_debug)**_


###Usage
To run Fluent-Bit with Logz.io output plugin:
```text
fluent-bit -e /fluent-bit/plugins/out_logzio.so -c /fluent-bit/etc/fluent-bit.conf
```

To run Fluent-Bit with Logz.io output plugin in docker:
```text
docker run -it --rm -v /path/to/fluent-bit.conf:/fluent-bit/etc/fluent-bit.conf logzio/fluent-bit-output
```

_**Make sure**_ that your new configuration file has `Plugins_File plugins.conf` under `[Service]` section.

##Development

###Requirements

* Go version >= 1.11.x


###Download

Download the project:
```text
git clone https://github.com/logzio/fluent-bit-logzio-output.git
```

Install dependencies:
```text
dep ensure -vendor-only
```

**_Remember_** to run and add unit tests. For end2end tests you can add your Logz.io parameters to the fluent-bit.conf and run:
```text
docker build -t bit -f test/Dockerfile .
docker run test
```

Then check your logs in Logz.io


**_Note_** Currently, we do not support multiple Logz.io outputs.
