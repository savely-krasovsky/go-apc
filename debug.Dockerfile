FROM golang:alpine AS build-env

# cache c deps
RUN apk update && apk add gcc libc-dev openssl-dev pkgconfig git

# cache go deps
RUN mkdir /go-apc
WORKDIR /go-apc
COPY go.mod go.sum ./

# install openssl dep to cache it
RUN go install github.com/spacemonkeygo/openssl

# compile Delve
RUN go install github.com/go-delve/delve/cmd/dlv

# download dependencies if go.sum changed
RUN go mod download
COPY . .

# build apcctl
RUN go build -gcflags "all=-N -l" -o ./bin/apcctl cmd/apcctl/main.go

# final stage
FROM alpine

# delve port
EXPOSE 2345

# allow delve to run on Alpine-based containers
RUN apk add libc6-compat

COPY --from=build-env /go-apc/bin/apcctl /
COPY --from=build-env /go/bin/dlv /
CMD ["/dlv", "--listen=:2345", "--headless=true", "--api-version=2", "exec", "/apcctl", "--", "-addr", "192.168.1.2:22700", "-agent-name", "testuser", "-password", "12345", "-headset-id", "32774", "-job-name", "TEST_JOB"]