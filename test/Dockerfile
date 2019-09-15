FROM golang:1.12 as gobuilder

ENV GOOS=linux\
    GOARCH=amd64

RUN mkdir /go/src/logzio \
    && go get -u github.com/golang/dep/cmd/dep

COPY / /go/src/logzio
WORKDIR /go/src/logzio

RUN dep ensure -v \
    && make all

FROM fluent/fluent-bit:1.1

COPY --from=gobuilder /go/src/logzio/build/out_logzio.so /fluent-bit/bin/
COPY --from=gobuilder /go/src/logzio/test/fluent-bit.conf /fluent-bit/etc/
COPY --from=gobuilder /go/src/logzio/test/plugins.conf /fluent-bit/etc/

EXPOSE 2020

CMD ["/fluent-bit/bin/fluent-bit", "--config", "/fluent-bit/etc/fluent-bit.conf"]
