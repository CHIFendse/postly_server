package internal

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"path"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	s3pb "postly/proto/s3"
)

// Server реализует сгенерированный интерфейс FileServiceServer
type Server struct {
	s3pb.UnimplementedFileServiceServer
	s3Storage *S3Client
}

// NewServer — конструктор gRPC-сервера
func NewServer(s3Storage *S3Client) *Server {
	return &Server{
		s3Storage: s3Storage,
	}
}

// UploadFile принимает поток байт (stream) от клиента и перенаправляет в Selectel S3
func (s *Server) UploadFile(stream s3pb.FileService_UploadFileServer) error {
	var fileName string
	fileBuffer := bytes.NewBuffer(nil)

	for {
		req, err := stream.Recv()

		if err == io.EOF {
			if fileName == "" {
				return status.Error(
					codes.InvalidArgument,
					"file name is missing",
				)
			}

			err := s.s3Storage.UploadFromReader(
				stream.Context(),
				fileBuffer,
				fileName,
			)
			if err != nil {
				return status.Errorf(
					codes.Internal,
					"failed to upload to S3: %v",
					err,
				)
			}

			url, err := s.s3Storage.GetPresignedURL(
				stream.Context(),
				fileName,
				time.Hour,
			)
			if err != nil {
				return status.Errorf(
					codes.Internal,
					"failed to generate file url: %v",
					err,
				)
			}

			return stream.SendAndClose(&s3pb.UploadFileResponse{
				S3Key:  fileName,
				FileUrl: url,
			})
		}

		if err != nil {
			return status.Errorf(
				codes.Unknown,
				"failed to receive stream: %v",
				err,
			)
		}

		switch data := req.Data.(type) {
		case *s3pb.UploadFileRequest_FileName:
			fileName = data.FileName

		case *s3pb.UploadFileRequest_Chunk:
			fileBuffer.Write(data.Chunk)
		}
	}
}

// GetDownloadUrl генерирует временную безопасную ссылку на приватный файл
func (s *Server) GetDownloadUrl(ctx context.Context, req *s3pb.GetDownloadUrlRequest) (*s3pb.GetDownloadUrlResponse, error) {
	if req.GetS3Key() == "" {
		return nil, status.Error(codes.InvalidArgument, "s3 key is required")
	}

	// Задаем время жизни ссылки (по умолчанию 1 час, если не передано иное)
	lifetime := time.Duration(req.GetExpiresInSec()) * time.Second
	if lifetime <= 0 {
		lifetime = time.Hour
	}

	url, err := s.s3Storage.GetPresignedURL(ctx, req.GetS3Key(), lifetime)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to presign url: %v", err)
	}

	return &s3pb.GetDownloadUrlResponse{Url: url}, nil
}

// Папки бакета, в которые разрешена загрузка файлов сообщений
var uploadFolders = map[string]bool{
	"images": true,
	"videos": true,
	"voices": true,
	"files":  true,
}

// GetUploadUrl выдаёт presigned PUT-ссылку. Ключ генерируется здесь и содержит
// user_id, чтобы gateway мог проверить, что клиент ссылается на свой файл.
func (s *Server) GetUploadUrl(ctx context.Context, req *s3pb.GetUploadUrlRequest) (*s3pb.GetUploadUrlResponse, error) {
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	if !uploadFolders[req.GetFolder()] {
		return nil, status.Errorf(codes.InvalidArgument, "unknown folder %q", req.GetFolder())
	}

	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to generate key: %v", err)
	}

	ext := strings.ToLower(path.Ext(req.GetFileName()))
	if len(ext) > 10 {
		ext = ""
	}
	key := req.GetFolder() + "/" + req.GetUserId() + "/" + hex.EncodeToString(id) + ext

	lifetime := time.Duration(req.GetExpiresInSec()) * time.Second
	if lifetime <= 0 {
		lifetime = 15 * time.Minute
	}

	url, err := s.s3Storage.GetPresignedPutURL(ctx, key, req.GetContentType(), lifetime)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to presign upload url: %v", err)
	}

	return &s3pb.GetUploadUrlResponse{UploadUrl: url, S3Key: key}, nil
}

// DeleteFile удаляет объект из Selectel
func (s *Server) DeleteFile(ctx context.Context, req *s3pb.DeleteFileRequest) (*s3pb.DeleteFileResponse, error) {
	if req.GetS3Key() == "" {
		return nil, status.Error(codes.InvalidArgument, "s3 key is required")
	}

	err := s.s3Storage.DeleteFile(ctx, req.GetS3Key())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete file: %v", err)
	}

	return &s3pb.DeleteFileResponse{Success: true}, nil
}
