# go-apc

[![GoDoc Widget](https://godoc.org/github.com/L11R/go-apc?status.svg)](https://godoc.org/github.com/L11R/go-apc)
[![Go Report](https://goreportcard.com/badge/github.com/L11R/go-apc)](https://goreportcard.com/report/github.com/L11R/go-apc)

The library to work with Avaya Proactive Control Agent API. Supports old versions with TLS 1.0 only support.
Due to incompatibility with BEAST patched clients Go `tls` package was [forked](https://github.com/L11R/apc-tls).

`cmd/apcctl` contains source code of the example utility that logins, attaches a job and receives events.

It lacks tests, but was battle-tested under real production loads without major changes.