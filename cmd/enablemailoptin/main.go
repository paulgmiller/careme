package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/users"
)

func main() {
	ctx := context.Background()
	if _, err := config.Load(); err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}
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
		if !strings.HasPrefix(user.ID, "user_") {
			continue
		}
		if user.FavoriteStore == "" || user.MailOptIn {
			continue
		}
		user.MailOptIn = true
		if err := storage.Update(&user); err != nil {
			log.Printf("failed to update user %s: %v", user.ID, err)
			continue
		}
		log.Printf("update user %s: %s: %v", user.ID, user.Email[0], user.FavoriteStore)

		updated++
	}
	fmt.Printf("enabled mail_opt_in for %d users\n", updated)
}
