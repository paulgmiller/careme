// migraterecipe copies a recipe, its image, and its wine pairing between Azure accounts.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"careme/internal/cache"
	"careme/internal/recipes"

	"github.com/joho/godotenv"
)

type storage struct {
	account string
	recipes cache.Cache
	images  cache.Cache
}

func main() {
	sourceEnv := flag.String("source-env", ".envtest", "Source Azure credentials file")
	destinationEnv := flag.String("destination-env", ".envprod", "Destination Azure credentials file")
	hash := flag.String("hash", "", "Recipe hash to copy (unchanged)")
	apply := flag.Bool("apply", false, "Copy records; default only previews")
	flag.Parse()
	if flag.NArg() != 0 || validateHash(*hash) != nil {
		log.Fatal("provide -hash with a single recipe hash and no positional arguments")
	}
	source, err := openStorage(*sourceEnv)
	if err != nil {
		log.Fatal(err)
	}
	destination, err := openStorage(*destinationEnv)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := migrate(ctx, source, destination, *hash, *apply, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func openStorage(path string) (storage, error) {
	values, err := godotenv.Read(path)
	if err != nil {
		// Parser errors can contain credential lines; do not print them.
		return storage{}, fmt.Errorf("cannot read credentials file %q; check its path, permissions, and dotenv syntax", path)
	}
	account := values["AZURE_STORAGE_ACCOUNT_NAME"]
	key := values["AZURE_STORAGE_PRIMARY_ACCOUNT_KEY"]
	recipeCache, err := cache.NewBlobCacheForAccount("recipes", account, key, http.DefaultTransport)
	if err != nil {
		return storage{}, fmt.Errorf("configure recipes storage from %q: %w", path, err)
	}
	imageCache, err := cache.NewBlobCacheForAccount(recipes.RecipeImagesContainer, account, key, http.DefaultTransport)
	if err != nil {
		return storage{}, fmt.Errorf("configure image storage from %q: %w", path, err)
	}
	return storage{account: account, recipes: recipeCache, images: imageCache}, nil
}

func validateHash(hash string) error {
	if hash == "" || strings.ContainsAny(hash, "/\\ \t\r\n") || hash == "." || hash == ".." {
		return fmt.Errorf("a single recipe hash is required")
	}
	return nil
}

func migrate(ctx context.Context, source, destination storage, hash string, apply bool, out io.Writer) error {
	if err := validateHash(hash); err != nil {
		return err
	}
	if source.account == destination.account {
		return fmt.Errorf("source and destination storage accounts must differ")
	}
	// Publish the recipe last so its dependencies are present when it becomes visible.
	entries := []struct {
		container string
		key       string
		source    cache.Cache
		target    cache.Cache
		data      []byte
		exists    bool
	}{
		{container: recipes.RecipeImagesContainer, key: "recipes/" + hash, source: source.images, target: destination.images},
		{container: "recipes", key: "wine_recommendations/" + hash, source: source.recipes, target: destination.recipes},
		{container: "recipes", key: "recipe/" + hash, source: source.recipes, target: destination.recipes},
	}
	// Fully read all required source records and check conflicts before any writes.
	for i := range entries {
		e := &entries[i]
		data, err := readRecord(ctx, e.source, e.key)
		if err != nil {
			return fmt.Errorf("read source %s/%s: %w", e.container, e.key, err)
		}
		e.data = data
		existing, err := readRecord(ctx, e.target, e.key)
		switch {
		case errors.Is(err, cache.ErrNotFound):
		case err != nil:
			return fmt.Errorf("read destination %s/%s: %w", e.container, e.key, err)
		case !bytes.Equal(existing, data):
			return fmt.Errorf("destination conflict at %s/%s; refusing to overwrite", e.container, e.key)
		default:
			e.exists = true
		}
	}
	_, _ = fmt.Fprintf(out, "%s -> %s (recipe %s)\n", source.account, destination.account, hash)
	for _, e := range entries {
		if e.exists {
			_, _ = fmt.Fprintf(out, "identical %s/%s\n", e.container, e.key)
			continue
		}
		if !apply {
			_, _ = fmt.Fprintf(out, "would copy %s/%s (%d bytes)\n", e.container, e.key, len(e.data))
			continue
		}
		if err := e.target.PutReader(ctx, e.key, bytes.NewReader(e.data), cache.IfNoneMatch()); err != nil {
			return fmt.Errorf("copy %s/%s: %w; earlier copies may remain, rerun to resume", e.container, e.key, err)
		}
		got, err := readRecord(ctx, e.target, e.key)
		if err != nil {
			return fmt.Errorf("verify %s/%s: %w; earlier copies may remain", e.container, e.key, err)
		}
		if !bytes.Equal(got, e.data) {
			return fmt.Errorf("verify %s/%s: destination bytes differ", e.container, e.key)
		}
		_, _ = fmt.Fprintf(out, "copied and verified %s/%s\n", e.container, e.key)
	}
	return nil
}

func readRecord(ctx context.Context, c cache.Cache, key string) ([]byte, error) {
	r, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(r)
	return data, errors.Join(readErr, r.Close())
}
