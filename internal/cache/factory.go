package cache

import (
	"fmt"
	"log"
	"os"
)

// TODO take a config? let it set container or directory?
func MakeCache() (ListCache, error) {
	if _, ok := os.LookupEnv("AZURE_COSMOS_ENDPOINT"); ok {
		db := os.Getenv("AZURE_COSMOS_DATABASE")
		if db == "" {
			return nil, fmt.Errorf("AZURE_COSMOS_DATABASE could not be found")
		}
		container := os.Getenv("AZURE_COSMOS_CONTAINER")
		if container == "" {
			container = "recipes"
		}
		log.Println("Using Azure Cosmos DB for cache")
		return NewCosmosCache(db, container)
	}

	if _, ok := os.LookupEnv("AZURE_STORAGE_ACCOUNT_NAME"); ok {
		log.Println("Using Azure Blob Storage for cache")
		return NewBlobCache("recipes")
	}

	return NewFileCache("cache"), nil
}
