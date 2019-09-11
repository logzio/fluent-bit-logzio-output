# Logz.io-Out Plugin for Fluent Bit

\*\*\*\* **this will be replaced with the doc contents once it's approved** \*\*\*\*

## Contributing to the project

**Requirements**:

* Go version >= 1.11.x

To contribute, clone this repo
and install dependencies

```shell
dep ensure -vendor-only
```

Remember to run and add unit tests. For end-to-end tests, you can add your Logz.io parameters to `fluent-bit.conf` and run:

```shell
docker build -t bit -f test/Dockerfile .
docker run test
```

Always confirm your logs are arriving at your Logz.io account.

