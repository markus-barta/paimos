// PAIMOS — Your Professional & Personal AI Project OS
// Copyright (C) 2026 Markus Barta <markus@barta.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, version 3.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see <https://www.gnu.org/licenses/>.

package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/markus-barta/paimos/backend/brand"
)

var Client *minio.Client
var Bucket string

// Init creates the MinIO client and ensures the bucket exists.
// Returns nil if MINIO_ENDPOINT is not set (feature disabled).
func Init() error {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		return nil // MinIO not configured — attachments disabled
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	Bucket = os.Getenv("MINIO_BUCKET")
	if Bucket == "" {
		Bucket = brand.Default.MinIOBucket
	}
	useSSL := os.Getenv("MINIO_USE_SSL") == "true"

	var err error
	Client, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("minio client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, err := Client.BucketExists(ctx, Bucket)
	if err != nil {
		return fmt.Errorf("minio bucket check: %w", err)
	}
	if !exists {
		if err := Client.MakeBucket(ctx, Bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("minio make bucket: %w", err)
		}
	}
	return nil
}

// Enabled returns true if MinIO is configured.
func Enabled() bool { return Client != nil }

// Put uploads data to MinIO and returns the object key.
func Put(ctx context.Context, key, contentType string, reader io.Reader, size int64) error {
	_, err := Client.PutObject(ctx, Bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	return err
}

// Get returns a reader for the object along with its content type and
// actual byte size. Callers MUST use the returned size for any
// `Content-Length` header — the DB's `size_bytes` column records the
// pre-processing upload size, which diverges from the stored object's
// size once image processing (resize / re-encode) has run. Using the
// DB value would send `Content-Length` that doesn't match the body and
// surface as broken images (`ERR_CONTENT_LENGTH_MISMATCH` in browsers,
// silent truncation via some CDNs).
func Get(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	obj, err := Client.GetObject(ctx, Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", 0, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, "", 0, err
	}
	return obj, info.ContentType, info.Size, nil
}

// Delete removes an object from MinIO.
func Delete(ctx context.Context, key string) error {
	return Client.RemoveObject(ctx, Bucket, key, minio.RemoveObjectOptions{})
}
