package blob

import (
	"context"
	"fmt"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures an S3-compatible bucket (AWS, MinIO, Hetzner, …).
type S3Config struct {
	Endpoint  string // host[:port], e.g. fsn1.your-objectstorage.com
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string // optional key prefix, e.g. "phoedox/"
	Insecure  bool   // plain HTTP (only for local test setups)
}

// S3 stores blobs in a bucket.
type S3 struct {
	c      *minio.Client
	bucket string
	prefix string
}

// NewS3 connects to the bucket (it must exist).
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("blob: S3 needs endpoint and bucket")
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: !cfg.Insecure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	p := cfg.Prefix
	if p != "" && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return &S3{c: c, bucket: cfg.Bucket, prefix: p}, nil
}

func (s *S3) key(hash string) string { return s.prefix + key(hash) }

func (s *S3) Put(ctx context.Context, hash, path string, _ int64) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	if _, err := s.c.StatObject(ctx, s.bucket, s.key(hash), minio.StatObjectOptions{}); err == nil {
		return nil
	}
	_, err := s.c.FPutObject(ctx, s.bucket, s.key(hash), path, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *S3) Open(ctx context.Context, hash string) (ReadSeekCloser, error) {
	if err := checkHash(hash); err != nil {
		return nil, err
	}
	o, err := s.c.GetObject(ctx, s.bucket, s.key(hash), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := o.Stat(); err != nil {
		o.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return o, nil
}

func (s *S3) Delete(ctx context.Context, hash string) error {
	if err := checkHash(hash); err != nil {
		return err
	}
	return s.c.RemoveObject(ctx, s.bucket, s.key(hash), minio.RemoveObjectOptions{})
}
