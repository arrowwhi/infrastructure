package main

import (
	"context"
	"fmt"

	httpclient "github.com/arrowwhi/infrastructure/http/client"
)

type Post struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	UserID int    `json:"userId"`
}

func main() {
	ctx := context.Background()

	cfg := httpclient.Config{
		BaseURL: "https://jsonplaceholder.typicode.com",
	}

	httpClient, err := httpclient.NewClient(cfg,
		httpclient.WithHeader("Accept", "application/json"),
		httpclient.WithHeader("User-Agent", "go-http-client/1.0"),
		httpclient.WithHeader("X-Request-ID", "example-request-123"),
	)
	if err != nil {
		fmt.Printf("Failed to create HTTP client: %v\n", err)
		return
	}

	if err := exampleGetRequest(ctx, httpClient); err != nil {
		fmt.Printf("GET Request Error: %v\n", err)
		return
	}

	if err := examplePostRequest(ctx, httpClient); err != nil {
		fmt.Printf("POST Request Error: %v\n", err)
		return
	}
}

func exampleGetRequest(ctx context.Context, httpClient httpclient.HTTPClient) error {
	fmt.Println("=== GET Request Example ===")

	var post Post
	resp, err := httpClient.Get(ctx, "/posts/1").
		SetHeader("X-Custom-Header", "get-request").
		SetQueryParam("_limit", 1).
		SetQueryParam("_sort", "id").
		Execute(&post)

	if err != nil {
		return fmt.Errorf("execute GET request: %w", err)
	}

	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	fmt.Printf("Post ID: %d\n", post.ID)
	fmt.Printf("Post Title: %s\n", post.Title)
	if len(post.Body) > 50 {
		fmt.Printf("Post Body: %s...\n", post.Body[:50])
	} else {
		fmt.Printf("Post Body: %s\n", post.Body)
	}

	return nil
}

func examplePostRequest(ctx context.Context, httpClient httpclient.HTTPClient) error {
	fmt.Println("\n=== POST Request Example ===")

	postData := Post{
		Title:  "foo",
		Body:   "bar",
		UserID: 1,
	}

	var createdPost Post
	resp, err := httpClient.Post(ctx, "/posts").
		SetHeader("Content-Type", "application/json").
		SetHeader("X-Custom-Header", "post-request").
		SetHeader("X-Client-Version", "1.0.0").
		SetQueryParam("_delay", 100).
		SetBody(postData).
		Execute(&createdPost)

	if err != nil {
		return fmt.Errorf("execute POST request: %w", err)
	}

	fmt.Printf("POST Status Code: %d\n", resp.StatusCode)
	fmt.Printf("Created Post ID: %d\n", createdPost.ID)
	fmt.Printf("Created Post Title: %s\n", createdPost.Title)

	return nil
}
