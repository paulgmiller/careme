package main

import (
	"context"
	"fmt"
	"log"

	"careme/internal/cache"
	"careme/internal/users"
)

func main() {
	ctx := context.Background()
	c, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}
	storage := users.NewStorage(c)
	allUsers, err := storage.List(ctx)
	if err != nil {
		log.Fatalf("failed to list users: %v", err)
	}

	updated := 0
	for i := range allUsers {
		user := allUsers[i]
		if user.FavoriteStore == "" || user.MailOptIn {
			continue
		}
		user.MailOptIn = true
		if err := storage.Update(&user); err != nil {
			log.Fatalf("failed to update user %s: %v", user.ID, err)
		}
		updated++
	}
	fmt.Printf("enabled mail_opt_in for %d users\n", updated)
}
