package main

import (
	"careme/internal/cache"
	"careme/internal/users"
	"context"
	"flag"
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
	var old users.User
	var new []users.User
	for _, u := range userList {
		//if !slices.Contains(u.Email, userEmail) {
		//	continue
		//}
		log.Printf("user: %s, email: %s recipes: %d", u.ID, u.Email, len(u.LastRecipes))
		if isOld(u.ID) {
			old = u
		} else {
			new = append(new, u)
		}
	}

	for _, n := range new {
		log.Printf("%s -> %s, email: %s ", old.ID, n.ID, n.Email)
		if move {
			n.LastRecipes = append(old.LastRecipes, n.LastRecipes...)
			n.FavoriteStore = old.FavoriteStore
			if err := userStorage.Update(&n); err != nil {
				log.Fatalf("failed to save user: %v", err)
			}
		}
	}

}

func isOld(userid string) bool {
	return !strings.HasPrefix(userid, "user_")
}
