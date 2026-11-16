// Package storage uploads citizen media to AWS S3.
package storage

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Uploader writes objects to one S3 bucket and returns their public URLs.
// The bucket (or a bucket policy) must allow public reads; the uploader does
// not set an ACL because many buckets have ACLs disabled.
type Uploader struct {
	client *s3.Client
	bucket string
	region string
}

// NewS3Uploader builds an uploader. When accessKeyID/secretAccessKey are
// empty, the AWS default credential chain is used instead.
func NewS3Uploader(ctx context.Context, region, bucket, accessKeyID, secretAccessKey string) (*Uploader, error) {
	region = strings.TrimSpace(region)
	bucket = strings.TrimSpace(bucket)
	if region == "" {
		return nil, fmt.Errorf("s3: region is empty")
	}
	if bucket == "" {
		return nil, fmt.Errorf("s3: bucket is empty")
	}

	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	accessKeyID = strings.TrimSpace(accessKeyID)
	secretAccessKey = strings.TrimSpace(secretAccessKey)
	if accessKeyID != "" || secretAccessKey != "" {
		if accessKeyID == "" || secretAccessKey == "" {
			return nil, fmt.Errorf("s3: both AWS access key and secret are required")
		}
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		))
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("s3: load config: %w", err)
	}
	return &Uploader{
		client: s3.NewFromConfig(cfg),
		bucket: bucket,
		region: region,
	}, nil
}

// Upload stores body at key and returns the object's public HTTPS URL.
func (u *Uploader) Upload(ctx context.Context, key string, body []byte, contentType string) (string, error) {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return "", fmt.Errorf("s3: key is empty")
	}
	if len(body) == 0 {
		return "", fmt.Errorf("s3: body is empty")
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/octet-stream"
	}

	_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(u.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(body),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
	})
	if err != nil {
		return "", fmt.Errorf("s3: put %s: %w", key, err)
	}
	return u.PublicURL(key), nil
}

// PublicURL returns the virtual-hosted HTTPS URL for key.
func (u *Uploader) PublicURL(key string) string {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	host := fmt.Sprintf("%s.s3.%s.amazonaws.com", u.bucket, u.region)
	if u.region == "us-east-1" {
		host = fmt.Sprintf("%s.s3.amazonaws.com", u.bucket)
	}
	return "https://" + host + "/" + escapeKey(key)
}

// AudioExtension maps a WhatsApp audio MIME type to a file extension.
func AudioExtension(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch mimeType {
	case "audio/ogg", "audio/oga":
		return ".ogg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a":
		return ".m4a"
	case "audio/mpeg":
		return ".mp3"
	case "audio/aac":
		return ".aac"
	case "audio/amr":
		return ".amr"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/webm":
		return ".webm"
	default:
		return ".bin"
	}
}

// AudioKey builds a unique, path-safe object key for an inbound voice note.
func AudioKey(phone, messageID, mimeType string, now time.Time) string {
	return fmt.Sprintf(
		"complaints/audio/%s/%s-%s%s",
		keyPart(phone),
		now.UTC().Format("20060102T150405"),
		keyPart(messageID),
		AudioExtension(mimeType),
	)
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func keyPart(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	if len(out) > 80 {
		return out[:80]
	}
	return out
}
