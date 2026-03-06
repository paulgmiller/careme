package main

import (
	"careme/internal/ai"
	"careme/internal/safewayads"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	var (
		startStore int
		endStore   int
		delay      time.Duration
		limit      int
		resume     bool
		extract    bool
	)

	flag.IntVar(&startStore, "start-store", 1, "first store ID to attempt")
	flag.IntVar(&endStore, "end-store", 500, "last store ID to attempt")
	flag.DurationVar(&delay, "delay", 5*time.Second, "delay between store attempts")
	flag.IntVar(&limit, "limit", 0, "maximum number of stores to process (0 = all)")
	flag.BoolVar(&resume, "resume", true, "skip stores already marked success")
	flag.BoolVar(&extract, "extract", true, "run OpenAI ingredient extraction after downloading the image")
	flag.Parse()

	if endStore < startStore {
		log.Fatalf("end-store must be >= start-store")
	}
	if extract && os.Getenv("AI_API_KEY") == "" {
		log.Fatalf("AI_API_KEY must be set when -extract=true")
	}

	storage, err := safewayads.NewStorageFromEnv()
	if err != nil {
		log.Fatalf("failed to create storage: %v", err)
	}

	httpClient := &http.Client{Timeout: 45 * time.Second}
	client := safewayads.NewClient(httpClient)

	var aiClient *ai.Client
	if extract {
		aiClient = ai.NewClient(os.Getenv("AI_API_KEY"), "")
	}

	ctx := context.Background()
	processed := 0
	for id := startStore; id <= endStore; id++ {
		if limit > 0 && processed >= limit {
			break
		}
		storeID := safewayads.FormatStoreID(id)

		if resume {
			status, err := loadStatus(ctx, storage, storeID)
			if err == nil && status.Status == "success" {
				log.Printf("skipping store %s due to resume", storeID)
				continue
			}
		}

		if err := processStore(ctx, storage, client, aiClient, storeID, extract); err != nil {
			log.Printf("store %s failed: %v", storeID, err)
		}

		processed++
		if delay > 0 && id < endStore && (limit == 0 || processed < limit) {
			time.Sleep(delay)
		}
	}
}

func processStore(ctx context.Context, storage safewayads.Storage, client *safewayads.Client, aiClient *ai.Client, storeID string, extract bool) error {
	now := time.Now().UTC()
	status := safewayads.RunStatus{
		StoreID:   storeID,
		StoreCode: safewayads.CanonicalStoreCode(storeID),
		UpdatedAt: now,
		Status:    "scrape_error",
	}

	result, err := client.FetchWeeklyAdAssets(ctx, storeID)
	if err != nil {
		if errors.Is(err, safewayads.ErrNoAd) {
			status.Status = "no_ad"
			status.Error = err.Error()
			return saveStatus(ctx, storage, status)
		}
		status.Error = err.Error()
		_ = saveStatus(ctx, storage, status)
		return err
	}

	status.StoreName = result.StoreName
	status.City = result.City
	status.State = result.State
	status.PostalCode = result.PostalCode
	status.SourcePageURL = result.SourcePageURL
	status.PublicationID = result.Publication.ID
	status.PublicationName = result.Publication.ExternalDisplayName
	if status.PublicationName == "" {
		status.PublicationName = result.Publication.Name
	}
	status.PDFURL = result.PDFURL
	status.PageCount = len(result.Pages)
	status.ImageURL = result.ImageURL

	pageAssets := make([]safewayads.PageAsset, 0, len(result.Pages))
	for _, page := range result.Pages {
		imageKey := safewayads.PageImageKey(storeID, result.Publication.ID, page.PageNumber, page.ImageURL, page.ContentType, page.ImageBytes, now)
		if err := storage.PutBytes(ctx, imageKey, page.ImageBytes, page.ContentType); err != nil {
			status.Error = fmt.Sprintf("upload page %d image: %v", page.PageNumber, err)
			_ = saveStatus(ctx, storage, status)
			return err
		}
		pageAsset := safewayads.PageAsset{
			PageNumber:       page.PageNumber,
			ImageURL:         page.ImageURL,
			ImageKey:         imageKey,
			ImageChecksum:    page.Checksum,
			ImageContentType: page.ContentType,
		}
		pageAssets = append(pageAssets, pageAsset)
		if page.PageNumber == 1 {
			status.ImageKey = imageKey
			status.ImageChecksum = page.Checksum
			status.ImageContentType = page.ContentType
			if status.ImageURL == "" {
				status.ImageURL = page.ImageURL
			}
		}
	}
	status.Pages = pageAssets
	if status.ImageKey == "" && len(pageAssets) > 0 {
		status.ImageKey = pageAssets[0].ImageKey
		status.ImageChecksum = pageAssets[0].ImageChecksum
		status.ImageContentType = pageAssets[0].ImageContentType
	}

	if !extract {
		status.Status = "image_only"
		return saveStatus(ctx, storage, status)
	}

	var ingredients []ai.WeeklyAdIngredient
	for _, page := range result.Pages {
		pageIngredients, err := aiClient.ExtractWeeklyAdIngredients(ctx, page.ImageBytes, page.ContentType)
		if err != nil {
			status.Status = "extract_error"
			status.Error = fmt.Sprintf("extract page %d: %v", page.PageNumber, err)
			_ = saveStatus(ctx, storage, status)
			return err
		}
		for i := range pageIngredients {
			pageIngredients[i].PageNumber = page.PageNumber
		}
		ingredients = append(ingredients, pageIngredients...)
	}

	ingredientsKey := safewayads.IngredientsKey(storeID, result.Publication.ID, now)
	doc := safewayads.IngredientDocument[ai.WeeklyAdIngredient]{
		StoreID:         storeID,
		StoreCode:       result.StoreCode,
		PublicationID:   result.Publication.ID,
		PublicationName: status.PublicationName,
		ExtractedAt:     now,
		PDFURL:          result.PDFURL,
		PageCount:       len(pageAssets),
		Pages:           pageAssets,
		ImageURL:        result.ImageURL,
		ImageKey:        status.ImageKey,
		ImageChecksum:   status.ImageChecksum,
		Ingredients:     ingredients,
	}
	if err := storage.PutJSON(ctx, ingredientsKey, doc); err != nil {
		status.Status = "extract_error"
		status.Error = fmt.Sprintf("save ingredients: %v", err)
		_ = saveStatus(ctx, storage, status)
		return err
	}

	status.Status = "success"
	status.IngredientsKey = ingredientsKey
	status.IngredientCount = len(ingredients)
	status.Error = ""
	return saveStatus(ctx, storage, status)
}

func loadStatus(ctx context.Context, storage safewayads.Storage, storeID string) (*safewayads.RunStatus, error) {
	var status safewayads.RunStatus
	if err := storage.GetJSON(ctx, safewayads.StatusKey(storeID), &status); err != nil {
		return nil, err
	}
	return &status, nil
}

func saveStatus(ctx context.Context, storage safewayads.Storage, status safewayads.RunStatus) error {
	status.UpdatedAt = time.Now().UTC()
	return storage.PutJSON(ctx, safewayads.StatusKey(status.StoreID), status)
}
