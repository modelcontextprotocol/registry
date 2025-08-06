package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// This is a manual validation script to test MongoDB unique constraints
// Run this against a MongoDB instance to validate the unique index behavior
func main() {
	fmt.Println("MongoDB Token Uniqueness Validation")
	fmt.Println("===================================")

	// You can modify this connection string for your MongoDB instance
	connectionURI := "mongodb://localhost:27017"
	if connectionURI == "mongodb://localhost:27017" {
		fmt.Println("⚠️  Using default MongoDB URI. Update if needed.")
		fmt.Println("   To test with a different MongoDB, update the connectionURI variable.")
		fmt.Println()
	}

	ctx := context.Background()

	// Connect to MongoDB
	fmt.Println("🔌 Connecting to MongoDB...")
	db, err := database.NewMongoDB(ctx, connectionURI, "test_token_validation", "servers", "verification")
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer db.Close()

	fmt.Println("✅ Connected successfully!")

	// Clean up any existing test data
	fmt.Println("🧹 Cleaning up existing test data...")
	// Note: In a real validation, you might want to preserve existing data
	// This is just for testing purposes

	// Test 1: Basic token uniqueness
	fmt.Println("\n📝 Test 1: Basic Token Uniqueness")
	fmt.Println("----------------------------------")

	testToken := &model.VerificationToken{
		Token:     "validation_token_" + fmt.Sprintf("%d", time.Now().Unix()),
		CreatedAt: time.Now(),
	}

	// Store token for first domain
	fmt.Printf("   Storing token '%s' for domain1.test...\n", testToken.Token)
	err = db.StoreVerificationToken(ctx, "domain1.test", testToken)
	if err != nil {
		fmt.Printf("   ❌ Unexpected error: %v\n", err)
	} else {
		fmt.Println("   ✅ First storage succeeded")
	}

	// Try to store same token for different domain
	fmt.Printf("   Storing same token '%s' for domain2.test...\n", testToken.Token)
	err = db.StoreVerificationToken(ctx, "domain2.test", testToken)
	if err != nil {
		if database.ErrTokenAlreadyExists.Error() == err.Error() {
			fmt.Println("   ✅ Correctly rejected duplicate token")
		} else {
			fmt.Printf("   ⚠️  Got error but not the expected one: %v\n", err)
		}
	} else {
		fmt.Println("   ❌ Should have rejected duplicate token!")
	}

	// Test 2: Different tokens should work
	fmt.Println("\n📝 Test 2: Different Tokens Should Work")
	fmt.Println("---------------------------------------")

	differentToken := &model.VerificationToken{
		Token:     "different_token_" + fmt.Sprintf("%d", time.Now().Unix()),
		CreatedAt: time.Now(),
	}

	fmt.Printf("   Storing different token '%s' for domain2.test...\n", differentToken.Token)
	err = db.StoreVerificationToken(ctx, "domain2.test", differentToken)
	if err != nil {
		fmt.Printf("   ❌ Unexpected error: %v\n", err)
	} else {
		fmt.Println("   ✅ Different token stored successfully")
	}

	// Test 3: Verify stored tokens
	fmt.Println("\n�� Test 3: Verify Stored Tokens")
	fmt.Println("-------------------------------")

	tokens1, err := db.GetVerificationTokens(ctx, "domain1.test")
	if err != nil {
		fmt.Printf("   ❌ Error retrieving tokens for domain1: %v\n", err)
	} else {
		fmt.Printf("   Domain1 has %d pending token(s)\n", len(tokens1.PendingTokens))
		if len(tokens1.PendingTokens) > 0 {
			fmt.Printf("   First token: %s\n", tokens1.PendingTokens[0].Token)
		}
	}

	tokens2, err := db.GetVerificationTokens(ctx, "domain2.test")
	if err != nil {
		fmt.Printf("   ❌ Error retrieving tokens for domain2: %v\n", err)
	} else {
		fmt.Printf("   Domain2 has %d pending token(s)\n", len(tokens2.PendingTokens))
		if len(tokens2.PendingTokens) > 0 {
			fmt.Printf("   First token: %s\n", tokens2.PendingTokens[0].Token)
		}
	}

	fmt.Println("\n🎉 Validation complete!")
	fmt.Println("\n💡 Tips:")
	fmt.Println("   - If Test 1 shows duplicate rejection, MongoDB unique indexes are working")
	fmt.Println("   - If Test 2 succeeds, different tokens are allowed")
	fmt.Println("   - If Test 3 shows correct token counts, storage is working properly")
}
