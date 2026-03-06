package safewayads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

const (
	defaultContainer    = "recipes"
	defaultLocalRoot    = "cache"
	containerEnvVarName = "SAFEWAYADS_STORAGE_CONTAINER"
)

type Storage interface {
	Exists(ctx context.Context, key string) (bool, error)
	PutBytes(ctx context.Context, key string, data []byte, contentType string) error
	PutJSON(ctx context.Context, key string, value any) error
	GetJSON(ctx context.Context, key string, value any) error
}

func NewStorageFromEnv() (Storage, error) {
	accountName := strings.TrimSpace(os.Getenv("AZURE_STORAGE_ACCOUNT_NAME"))
	accountKey := strings.TrimSpace(os.Getenv("AZURE_STORAGE_PRIMARY_ACCOUNT_KEY"))
	container := storageContainerName()
	if accountName != "" && accountKey != "" {
		cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)
		if err != nil {
			return nil, fmt.Errorf("create azure credential: %w", err)
		}
		client, err := azblob.NewClientWithSharedKeyCredential(
			fmt.Sprintf("https://%s.blob.core.windows.net/", accountName),
			cred,
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("create azure blob client: %w", err)
		}
		return &azureStorage{client: client, container: container}, nil
	}
	return &fileStorage{root: defaultLocalRoot}, nil
}

func storageContainerName() string {
	container := strings.TrimSpace(os.Getenv(containerEnvVarName))
	if container == "" {
		return defaultContainer
	}
	return container
}

type fileStorage struct {
	root string
}

func (s *fileStorage) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(key)))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *fileStorage) PutBytes(_ context.Context, key string, data []byte, _ string) error {
	path := filepath.Join(s.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *fileStorage) PutJSON(ctx context.Context, key string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return s.PutBytes(ctx, key, data, "application/json")
}

func (s *fileStorage) GetJSON(_ context.Context, key string, value any) error {
	data, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(key)))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

type azureStorage struct {
	client    *azblob.Client
	container string
}

func (s *azureStorage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.ServiceClient().NewContainerClient(s.container).NewBlobClient(key).GetProperties(ctx, nil)
	if err == nil {
		return true, nil
	}
	if bloberror.HasCode(err, bloberror.BlobNotFound) {
		return false, nil
	}
	return false, err
}

func (s *azureStorage) PutBytes(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.UploadStream(ctx, s.container, key, bytes.NewReader(data), &azblob.UploadStreamOptions{
		HTTPHeaders: &blob.HTTPHeaders{
			BlobContentType: &contentType,
		},
	})
	if err != nil {
		return fmt.Errorf("upload blob %s: %w", key, err)
	}
	return nil
}

func (s *azureStorage) PutJSON(ctx context.Context, key string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return s.PutBytes(ctx, key, data, "application/json")
}

func (s *azureStorage) GetJSON(ctx context.Context, key string, value any) error {
	resp, err := s.client.DownloadStream(ctx, s.container, key, nil)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
