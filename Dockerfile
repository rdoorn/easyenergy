FROM golang:1.16-alpine as builder
RUN mkdir /build
ADD . /build/
WORKDIR /build
RUN apk add --no-cache git
RUN apk add --no-cache ca-certificates
ENV GOPATH /go/
ENV GOBIN /go/bin
RUN go get ./...
#RUN go mod download
#RUN go mod vendor
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags '-extldflags "-static"' -o easyenergy .
FROM scratch
COPY --from=builder /build/easyenergy /app/
COPY --from=builder /etc/ssl/certs /etc/ssl/certs
WORKDIR /app
CMD [ "./easyenergy" ]
