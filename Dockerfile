FROM golang:alpine AS build-env

# cache c deps
RUN apk update && apk add gcc libc-dev openssl-dev pkgconfig

# cache go deps
RUN mkdir /go-apc
WORKDIR /go-apc
COPY go.mod go.sum ./

# install openssl dep to cache it
RUN go install github.com/spacemonkeygo/openssl

# download dependencies if go.sum changed
RUN go mod download
COPY . .

# build apcctl
RUN go build -o ./bin/apcctl cmd/apcctl/main.go

# final stage
FROM alpine
COPY --from=build-env /go-apc/bin/apcctl /
CMD ["/apcctl", "-addr", "192.168.1.2:22700", "-agent-name", "movchan", "-password", "12345", "-headset-id", "32774", "-job-name", "TEST_DIT"]