package cache

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

type BlobCache struct {
	container *container.Client
}

var _ ListCache = (*BlobCache)(nil)

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

	containerClient := client.ServiceClient().NewContainerClient(container)

	return &BlobCache{
		container: containerClient,
	}, nil
}

// come back and use iterators or a queue?
func (fc *BlobCache) List(ctx context.Context, prefix string, _ string) ([]string, error) {
	var keys []string
	pager := fc.container.NewListBlobsFlatPager(&azblob.ListBlobsFlatOptions{
		Prefix: &prefix,
	})

	for pager.More() {

		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get next page of blobs: %w", err)
		}
		for _, blob := range page.Segment.BlobItems {
			keys = append(keys, strings.TrimPrefix(*blob.Name, prefix))
		}
	}

	return keys, nil
}

func (fc *BlobCache) Exists(key string) (bool, error) {
	_, err := fc.container.NewBlobClient(key).GetProperties(context.TODO(), &blob.GetPropertiesOptions{})
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil

}

func (fc *BlobCache) Get(key string) (io.ReadCloser, error) {
	stream, err := fc.container.NewBlockBlobClient(key).DownloadStream(context.TODO(), &azblob.DownloadStreamOptions{})
	if err != nil {
		if bloberror.HasCode(err, bloberror.BlobNotFound) {
			return nil, ErrNotFound
		}
		log.Printf("failed to download blob: %v", err)
		return nil, err
	}

	return stream.Body, nil
}

func (fc *BlobCache) Set(key, value string) error {
	_, err := fc.container.NewBlockBlobClient(key).UploadStream(context.TODO(), strings.NewReader(value), &azblob.UploadStreamOptions{})
	return err
}

func MakeCache() (ListCache, error) {
	_, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME")
	if ok {
		log.Println("Using Azure Blob Storage for cache")
		return NewBlobCache("recipes")
	}
	return NewFileCache("cache"), nil
}
