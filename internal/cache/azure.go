package cache

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

type BlobCache struct {
	containerClient *azblob.Client
	container       string
}

var _ Cache = (*BlobCache)(nil)

func NewBlobCache(container string) (*BlobCache, error) {
	// Your account name and key can be obtained from the Azure Portal.
	accountName, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME")
	if !ok {
		return nil, fmt.Errorf("AZURE_STORAGE_ACCOUNT_NAME could not be found")
	}

	accountKey, ok := os.LookupEnv("AZURE_STORAGE_PRIMARY_ACCOUNT_KEY")
	if !ok {
		return nil, fmt.Errorf("AZURE_STORAGE_PRIMARY_ACCOUNT_KEY could not be found")
	}

	cred, err := azblob.NewSharedKeyCredential(accountName, accountKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create shared key credential: %w", err)
	}

	// The service URL for blob endpoints is usually in the form: http(s)://<account>.blob.core.windows.net/
	client, err := azblob.NewClientWithSharedKeyCredential(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob client: %w", err)
	}

	return &BlobCache{
		containerClient: client,
		container:       container,
	}, nil
}

func (fc *BlobCache) Get(key string) (string, bool) {
	stream, err := fc.containerClient.DownloadStream(context.TODO(), fc.container, key, &azblob.DownloadStreamOptions{})
	if err != nil {
		//TODO don't log if not found
		return "", false
	}

	data, err := io.ReadAll(stream.Body)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func (fc *BlobCache) Set(key, value string) error {
	_, err := fc.containerClient.UploadStream(context.TODO(), fc.container, key, strings.NewReader(value), &azblob.UploadStreamOptions{})
	return err
}

func MakeCache() (Cache, error) {
	_, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME")
	if ok {
		log.Println("Using Azure Blob Storage for cache")
		return NewBlobCache("recipes")
	}
	return NewFileCache("cache"), nil
}
