package main

import (
	"crypto/tls"
	"net/http"
)

func insecureLocalTransport() http.RoundTripper {
	return &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
}
