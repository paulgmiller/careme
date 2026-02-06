package main

import (
	"careme/internal/cache"
	"careme/internal/users"
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
)

func main() {
	var move bool
	var userEmail string
	flag.BoolVar(&move, "move", false, "Move ingredient to the top of the list")
	flag.StringVar(&userEmail, "email", "nobody", "email of user id to move")
	flag.Parse()
	ctx := context.Background()
	cache, err := cache.MakeCache()
	if err != nil {
		log.Fatalf("failed to create cache: %s", err)
	}

	userStorage := users.NewStorage(cache)
	userList, err := userStorage.List(ctx)
	if err != nil {
		log.Fatalf("failed to list users: %v", err)
	}
	log.Printf("found %d users", len(userList))
	log.Printf("looking for user with email containing \"%s\"", userEmail)
	usersMap := map[string]bool{}
	for _, u := range userList {
		log.Printf("user: %s, email: %s recipes: %d", u.ID, u.Email, len(u.LastRecipes))
		usersMap[u.Email[0]] = true
	}

	for email := range usersMap {
		fmt.Println(email)
	}

}

func isOld(userid string) bool {
	return !strings.HasPrefix(userid, "user_")
}
