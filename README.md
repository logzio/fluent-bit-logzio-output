# Logz.io-Out Plugin for Fluent Bit

You have two options when running Logz.io-Out Plugin for Fluent Bit:

* [Run as a standalone app](#standalone-config)
* [Run in a Docker container](#docker-config)

<div id="standalone-config">

## Run as a standalone app

### Configuration

#### 1.  Install Fluent Bit

If you haven't installed Fluent Bit yet,
you can build it from source
according to the [instructions from Fluent Bit](https://docs.fluentbit.io/manual/installation/getting-started-with-fluent-bit).

#### 2.  Install and configure the Logz.io plugin

For Linux:
```shell
wget -O /fluent-bit/plugins/out_logzio-linux.so \
    https://github.com/logzio/fluent-bit-logzio-output/raw/master/build/out_logzio-linux.so
```

For MacOS:
```shell
wget -O /fluent-bit/plugins/out_logzio-linux.so \
    https://github.com/logzio/fluent-bit-logzio-output/raw/master/build/out_logzio-macOS.so
```

For Windows:
```shell
wget https://github.com/logzio/fluent-bit-logzio-output/raw/master/build/out_logzio-windows.so
```

In your Fluent Bit configuration file (`fluent-bit.conf` by default),
add Logz.io as an output.

**Note**:
Logz.io-Out Plugin for Fluent Bit
supports one output stream to Logz.io.
We plan to add support for multiple streams in the future. <br>
In the meantime,
we recommend running a new instance for each output stream you need.

For a list of options, see the [configuration parameters](#config-params) below to add to the code block. 👇

```python
[OUTPUT]
    Name  logzio
    Match *
    Workers 1
    logzio_token <<SHIPPING-TOKEN>>
    logzio_url   https://<<LISTENER-HOST>>:8071
    id <<any string>>
```
#### 3.  Run Fluent Bit with the Logz.io plugin

```shell
fluent-bit -e /fluent-bit/plugins/out_logzio-linux.so \
-c /fluent-bit/etc/fluent-bit.conf
```

#### 4.  Check Logz.io for your logs

Give your logs some time to get from your system to ours, and then open [Kibana](https://app.logz.io/#/dashboard/kibana).

If you still don't see your logs, see [log shipping troubleshooting](https://docs.logz.io/user-guide/log-shipping/log-shipping-troubleshooting.html).

</div>

<div id="docker-config">

## Run in a Docker container

### Configuration

#### 1.  Make the configuration file

To run in a container,
create a configuration file named `fluent-bit.conf`.

**Note**:
Logz.io-Out Plugin for Fluent Bit
supports one output stream to Logz.io.
We plan to add support for multiple streams in the future. <br>
In the meantime,
we recommend running a new instance for each output stream you need.

For a list of options, see the [configuration parameters](#config-params) below to add to the code block. 👇

```python
[SERVICE]
    # Include your remaining SERVICE configuration here.
    Plugins_File plugins.conf

[OUTPUT]
    Name  logzio
    Match *
    logzio_token <<SHIPPING-TOKEN>>
    logzio_url   https://<<LISTENER-HOST>>:8071
    id <<any string>>
```
#### 2.  Run the Docker image

Run the Docker image
using the `fluent-bit.conf` file you made in step 1.

```shell
docker run -it --rm \
-v /path/to/fluent-bit.conf:/fluent-bit/etc/fluent-bit.conf \
logzio/fluent-bit-output
```

#### 3.  Check Logz.io for your logs

Give your logs some time to get from your system to ours, and then open [Kibana](https://app.logz.io/#/dashboard/kibana).

If you still don't see your logs, see [log shipping troubleshooting](https://docs.logz.io/user-guide/log-shipping/log-shipping-troubleshooting.html).

</div>

<div id="config-params">

## Output Parameters

| Parameter    | Description                                                                                                                                                                                                                                                                                                              |
|--------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| logzio_token | **Required**. Replace `<<SHIPPING-TOKEN>>` with the [token](https://app.logz.io/#/dashboard/settings/general) of the account you want to ship to.                                                                                                                                                                        |
| logzio_url   | **Default**: `https://listener.logz.io:8071`  Listener URL and port. Replace `<<LISTENER-HOST>>` with your region's listener host (for example, `listener.logz.io`). For more information on finding your account's region, see [Account region](https://docs.logz.io/user-guide/accounts/account-region.html). |
| logzio_type  | **Default**: `logzio-fluent-bit`  The [log type](https://docs.logz.io/user-guide/log-shipping/built-in-log-types.html), shipped as `type` field. Used by Logz.io for consistent parsing. Can't contain spaces.                                                                                                       |
| logzio_debug | **Default**: `false`  Set to `true` to print debug messages to stdout.                                                                                                                                                                                                                                               |
| id           | **Default**: `logzio_output_1`  Output id. Mandatory when using multiple outputs.                                                                                                                                                                                                                                    |
| dedot_enabled       | **Default**: `false`  Enabled dedot processing.                                                                                                                                                                                                                                                                      |
| dedot_nested        | **Default**: `false`  Enables nesting dedot processing.                                                                                                                                                                                                                                                              |
| dedot_new_seperator | **Default**: `"_"`  Seperator character to use when applying dedot processing.                                                                                                                                                                                                |
| proxy_host | **Optional**: `<PROXY_HOST>:<PROXY_PORT>`  Support HTTP proxy processing.                                                                                                                                                                                                |
| proxy_user | **Optional**: `""`  Support HTTP proxy user authentication.                                                                                                                                                                                                |
| proxy_pass | **Optional**: `""`  Support HTTP proxy password authentication.                                                                                                                                                          |

</div>

## Contributing to the project

**Requirements**:

* Go version >= 1.11.x

To contribute, clone this repo
and install dependencies

Remember to run and add unit tests. For end-to-end tests, you can add your Logz.io parameters to `fluent-bit.conf` and run:
Replace <<arch-type>> with amd or arm
```shell
docker build -t logzio-bit-test -f test/Dockerfile.<<arch-type>> .
docker run logzio-bit-test
```

Always confirm your logs are arriving at your Logz.io account.


## Change log
 - **0.5.0**:
    - Added HTTP proxy support.
    - Added Array fields support
    - Improved retries.
- **0.4.1**:
  - Trim the compiler build path from stack traces.
- **0.4.0**:
    - Add timestamp decode support for new fluentbit versions.
    - Update to fluent-bit `2.1.9` in docker image.
- **0.3.0**:
    - Added an optional dedot processing.
    - Upgraded to golang `1.19.1.` in docker image.
    - Update to fluent-bit `2.0.8` in docker image.
- **0.2.0**:
    - Added `id` parameter to support multiple outputs.
- **0.1.0**:
    - Upgrade to use Go modules (Thanks @camal-cakar-gcx)
    - Update to fluent-bit `1.8.3` in docker image.
    - Update to latest fluent-bit-go.
    - Upgrade to `gopkg.in/yaml.v3`.
- **0.0.2**:
    - Upgrade to fluent-bit 1.5.4 in docker image (Thanks @alysivji).
    - Fixed error output (Thanks @alexjurkiewicz).
- **0.0.1**:
    - Initial release.
