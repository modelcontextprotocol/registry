package main

import (
	"fmt"
	"log"
	"os"

	"github.com/modelcontextprotocol/registry/internal/verification"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: dns-verify <domain> <token>")
		fmt.Println("Example: dns-verify example.com TBeVXe_X4npM6p8vpzStnA")
		os.Exit(1)
	}

	domain := os.Args[1]
	token := os.Args[2]

	fmt.Printf("🔍 Verifying DNS record for domain: %s\n", domain)
	fmt.Printf("🎯 Expected token: %s\n", token)
	fmt.Printf("📋 Expected DNS record: mcp-verify=%s\n\n", token)

	// Perform DNS verification
	result, err := verification.VerifyDNSRecord(domain, token)
	if err != nil {
		log.Printf("❌ DNS verification error: %v", err)
		os.Exit(1)
	}

	// Display results
	fmt.Printf("📊 Verification Results:\n")
	fmt.Printf("   Success: %t\n", result.Success)
	fmt.Printf("   Domain: %s\n", result.Domain)
	fmt.Printf("   Token: %s\n", result.Token)
	fmt.Printf("   Duration: %s\n", result.Duration)
	fmt.Printf("   Message: %s\n", result.Message)

	if len(result.TXTRecords) > 0 {
		fmt.Printf("\n📝 Found TXT Records:\n")
		for i, record := range result.TXTRecords {
			fmt.Printf("   %d. %s\n", i+1, record)
		}
	}

	if result.Success {
		fmt.Println("\n✅ Domain verification successful!")
		os.Exit(0)
	} else {
		fmt.Println("\n❌ Domain verification failed!")
		os.Exit(1)
	}
}
