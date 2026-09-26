package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)


type S3Client struct {
	BucketName string
	client     *s3.Client
}


func NewS3Client() (*S3Client, error) {
	accountKey := os.Getenv("S3_ACCESS_KEY")
	accountSecret := os.Getenv("S3_SECRET_KEY")
	region := os.Getenv("S3_REGION")
	bucketName := os.Getenv("S3_BUCKET_NAME")

	host := "https://" + os.Getenv("S3_HOST")

	credProvider := credentials.NewStaticCredentialsProvider(accountKey, accountSecret, "")

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credProvider),
	)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки конфигурации S3: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = &host
		o.UsePathStyle = true
	})

	return &S3Client{
		BucketName: bucketName,
		client:     s3Client,
	}, nil
}


func (s *S3Client) UploadFromReader(ctx context.Context, reader io.Reader, s3ObjectKey string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.BucketName,
		Key:    &s3ObjectKey,
		Body:   reader,
	})
	if err != nil {
		return fmt.Errorf("ошибка загрузки потока в S3: %w", err)
	}
	return nil
}

// DownloadFile возвращает поток для скачивания файла
func (s *S3Client) DownloadFile(ctx context.Context, s3ObjectKey string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.BucketName,
		Key:    &s3ObjectKey,
	})
	if err != nil {
		return nil, fmt.Errorf("не удалось скачать файл из S3: %w", err)
	}
	return result.Body, nil
}

// DeleteFile удаляет файл из бакета Selectel
func (s *S3Client) DeleteFile(ctx context.Context, s3ObjectKey string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.BucketName,
		Key:    &s3ObjectKey,
	})
	if err != nil {
		return fmt.Errorf("ошибка удаления файла из S3: %w", err)
	}
	return nil
}

// newPresignClient подписывает ссылки в virtual-hosted стиле (bucket.host/key):
// Selectel отдаёт CORS-заголовки только на таком адресе, на path-style
// preflight получает 405 и браузер блокирует загрузку.
func (s *S3Client) newPresignClient() *s3.PresignClient {
	return s3.NewPresignClient(s.client, func(o *s3.PresignOptions) {
		o.ClientOptions = append(o.ClientOptions, func(o *s3.Options) {
			o.UsePathStyle = false
		})
	})
}

// GetPresignedURL генерирует временную безопасную ссылку на приватный файл
func (s *S3Client) GetPresignedURL(ctx context.Context, s3ObjectKey string, lifetime time.Duration) (string, error) {
	presignClient := s.newPresignClient()

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.BucketName,
		Key:    &s3ObjectKey,
	}, s3.WithPresignExpires(lifetime))
	
	if err != nil {
		return "", fmt.Errorf("не удалось создать преподписанную ссылку: %w", err)
	}

	return request.URL, nil
}

// GetPresignedPutURL генерирует временную ссылку для прямой загрузки файла клиентом
func (s *S3Client) GetPresignedPutURL(ctx context.Context, s3ObjectKey, contentType string, lifetime time.Duration) (string, error) {
	presignClient := s.newPresignClient()

	input := &s3.PutObjectInput{
		Bucket: &s.BucketName,
		Key:    &s3ObjectKey,
	}
	if contentType != "" {
		input.ContentType = &contentType
	}

	request, err := presignClient.PresignPutObject(ctx, input, s3.WithPresignExpires(lifetime))
	if err != nil {
		return "", fmt.Errorf("не удалось создать ссылку для загрузки: %w", err)
	}

	return request.URL, nil
}
