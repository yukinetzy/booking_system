package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd/role grant <email>")
	fmt.Println("  go run ./cmd/role revoke <email>")
	fmt.Println("  go run ./cmd/role show <email>")
	fmt.Println("  go run ./cmd/role list")
}

func main() {
	_ = godotenv.Load()

	action := strings.ToLower(strings.TrimSpace(argAt(1)))
	emailArg := strings.ToLower(strings.TrimSpace(argAt(2)))
	if action != "grant" && action != "revoke" && action != "show" && action != "list" {
		printUsage()
		os.Exit(1)
	}
	if (action == "grant" || action == "revoke" || action == "show") && emailArg == "" {
		fmt.Fprintln(os.Stderr, "Email is required for this action.")
		printUsage()
		os.Exit(1)
	}

	mongoURI := strings.TrimSpace(os.Getenv("MONGO_URI"))
	if mongoURI == "" {
		log.Fatal("MONGO_URI is required in .env")
	}

	dbName := strings.TrimSpace(os.Getenv("DB_NAME"))
	if dbName == "" {
		dbName = "easybook_final"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI).SetServerSelectionTimeout(15*time.Second))
	if err != nil {
		log.Fatalf("Role command failed: %v", err)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()

	users := client.Database(dbName).Collection("users")

	switch action {
	case "list":
		cursor, err := users.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"email": 1, "role": 1}).SetSort(bson.D{{Key: "email", Value: 1}}))
		if err != nil {
			log.Fatalf("Role command failed: %v", err)
		}
		defer cursor.Close(ctx)

		items := make([]bson.M, 0)
		if err := cursor.All(ctx, &items); err != nil {
			log.Fatalf("Role command failed: %v", err)
		}

		if len(items) == 0 {
			fmt.Println("No users found.")
			return
		}

		for _, item := range items {
			email := strings.TrimSpace(fmt.Sprint(item["email"]))
			role := strings.TrimSpace(fmt.Sprint(item["role"]))
			if role == "" || role == "<nil>" {
				role = "user"
			}
			fmt.Printf("%s | role=%s\n", defaultIfEmpty(email, "-"), role)
		}
		return
	}

	var user bson.M
	err = users.FindOne(ctx, bson.M{"email": emailArg}, options.FindOne().SetProjection(bson.M{"email": 1, "role": 1})).Decode(&user)
	if err == mongo.ErrNoDocuments {
		fmt.Fprintf(os.Stderr, "User not found: %s\n", emailArg)
		os.Exit(1)
	}
	if err != nil {
		log.Fatalf("Role command failed: %v", err)
	}

	email := strings.TrimSpace(fmt.Sprint(user["email"]))
	role := strings.TrimSpace(fmt.Sprint(user["role"]))
	if role == "" || role == "<nil>" {
		role = "user"
	}

	if action == "show" {
		fmt.Printf("%s | role=%s\n", defaultIfEmpty(email, "-"), role)
		return
	}

	targetRole := "user"
	if action == "grant" {
		targetRole = "admin"
	}

	if role == targetRole {
		fmt.Printf("No changes: %s already has role '%s'.\n", emailArg, targetRole)
		return
	}

	_, err = users.UpdateOne(ctx, bson.M{"email": emailArg}, bson.M{"$set": bson.M{"role": targetRole, "updatedAt": time.Now().UTC()}})
	if err != nil {
		log.Fatalf("Role command failed: %v", err)
	}

	fmt.Printf("Updated: %s -> role='%s'\n", emailArg, targetRole)
}

func argAt(index int) string {
	if len(os.Args) <= index {
		return ""
	}
	return os.Args[index]
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
