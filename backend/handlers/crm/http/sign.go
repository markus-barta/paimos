// PAIMOS -- Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.

package httpcrm

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	stdhttp "net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderTimestamp = "X-Paimos-Timestamp"
	HeaderSignature = "X-Paimos-Signature"

	signatureWindow = 5 * time.Minute
)

var (
	errMissingSignature = errors.New("missing HMAC signature")
	errBadTimestamp     = errors.New("invalid HMAC timestamp")
	errStaleTimestamp   = errors.New("HMAC timestamp outside allowed window")
	errBadSignature     = errors.New("invalid HMAC signature")
)

// ComputeSignature returns hex(HMAC-SHA256(secret, timestamp + "\n" + body)).
func ComputeSignature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// SignRequest attaches the PAIMOS HTTP sidecar authentication headers.
func SignRequest(req *stdhttp.Request, secret string, now time.Time, body []byte) {
	timestamp := strconv.FormatInt(now.Unix(), 10)
	req.Header.Set(HeaderTimestamp, timestamp)
	req.Header.Set(HeaderSignature, ComputeSignature(secret, timestamp, body))
}

// VerifyRequest verifies the PAIMOS sidecar authentication headers for a raw
// request body already read by the caller.
func VerifyRequest(req *stdhttp.Request, secret string, now time.Time, body []byte) error {
	return VerifySignature(
		secret,
		now,
		req.Header.Get(HeaderTimestamp),
		body,
		req.Header.Get(HeaderSignature),
	)
}

// VerifySignature checks timestamp freshness and compares the supplied
// signature in constant time.
func VerifySignature(secret string, now time.Time, timestamp string, body []byte, signature string) error {
	timestamp = strings.TrimSpace(timestamp)
	signature = strings.TrimSpace(signature)
	if timestamp == "" || signature == "" {
		return errMissingSignature
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %s", errBadTimestamp, timestamp)
	}
	signedAt := time.Unix(seconds, 0)
	if now.Sub(signedAt) > signatureWindow || signedAt.Sub(now) > signatureWindow {
		return errStaleTimestamp
	}
	want, err := hex.DecodeString(ComputeSignature(secret, timestamp, body))
	if err != nil {
		return err
	}
	got, err := hex.DecodeString(signature)
	if err != nil {
		return errBadSignature
	}
	if !hmac.Equal(got, want) {
		return errBadSignature
	}
	return nil
}
