package meilisearch

import (
	"context"
	"errors"
	"testing"
	"time"

	ms "github.com/meilisearch/meilisearch-go"
	"github.com/rs/zerolog"
	"github.com/stoa-hq/stoa/pkg/sdk"
)

// mockIndex implements indexManager for testing.
type mockIndex struct {
	searchFn          func(query string, request *ms.SearchRequest) (*ms.SearchResponse, error)
	addDocumentsFn    func(documents interface{}, primaryKey ...string) (*ms.TaskInfo, error)
	deleteDocumentFn  func(identifier string) (*ms.TaskInfo, error)
	updateSettingsFn  func(settings *ms.Settings) (*ms.TaskInfo, error)
}

func (m *mockIndex) Search(query string, request *ms.SearchRequest) (*ms.SearchResponse, error) {
	if m.searchFn != nil {
		return m.searchFn(query, request)
	}
	return &ms.SearchResponse{}, nil
}

func (m *mockIndex) AddDocuments(documents interface{}, primaryKey ...string) (*ms.TaskInfo, error) {
	if m.addDocumentsFn != nil {
		return m.addDocumentsFn(documents, primaryKey...)
	}
	return &ms.TaskInfo{}, nil
}

func (m *mockIndex) DeleteDocument(identifier string) (*ms.TaskInfo, error) {
	if m.deleteDocumentFn != nil {
		return m.deleteDocumentFn(identifier)
	}
	return &ms.TaskInfo{}, nil
}

func (m *mockIndex) UpdateSettings(settings *ms.Settings) (*ms.TaskInfo, error) {
	if m.updateSettingsFn != nil {
		return m.updateSettingsFn(settings)
	}
	return &ms.TaskInfo{}, nil
}

// mockClient implements clientProvider for testing.
type mockClient struct {
	indexes       map[string]*mockIndex
	waitForTaskFn func(taskUID int64, interval time.Duration) (*ms.Task, error)
}

func newMockClient() *mockClient {
	return &mockClient{indexes: make(map[string]*mockIndex)}
}

func (m *mockClient) Index(uid string) indexManager {
	if idx, ok := m.indexes[uid]; ok {
		return idx
	}
	// Return a default mock index.
	return &mockIndex{}
}

func (m *mockClient) WaitForTask(taskUID int64, interval time.Duration) (*ms.Task, error) {
	if m.waitForTaskFn != nil {
		return m.waitForTaskFn(taskUID, interval)
	}
	return &ms.Task{Status: ms.TaskStatusSucceeded}, nil
}

// Compile-time check.
var _ sdk.SearchEngine = (*MeilisearchEngine)(nil)

func TestMeilisearchEngine_Search(t *testing.T) {
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		searchFn: func(query string, req *ms.SearchRequest) (*ms.SearchResponse, error) {
			if query != "laptop" {
				t.Errorf("query = %q, want %q", query, "laptop")
			}
			if req.Offset != 10 {
				t.Errorf("Offset = %d, want 10", req.Offset)
			}
			if req.Limit != 10 {
				t.Errorf("Limit = %d, want 10", req.Limit)
			}
			return &ms.SearchResponse{
				Hits: []interface{}{
					map[string]interface{}{
						"entity_id":   "uuid-1",
						"name":        "Gaming Laptop",
						"description": "A great laptop",
						"slug":        "gaming-laptop",
					},
				},
				EstimatedTotalHits: 42,
			}, nil
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	resp, err := engine.Search(context.Background(), sdk.SearchRequest{
		Query: "laptop",
		Page:  2,
		Limit: 10,
		Types: []string{"product"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 42 {
		t.Errorf("Total = %d, want 42", resp.Total)
	}
	if resp.Page != 2 {
		t.Errorf("Page = %d, want 2", resp.Page)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results len = %d, want 1", len(resp.Results))
	}
	r := resp.Results[0]
	if r.ID != "uuid-1" {
		t.Errorf("ID = %q, want %q", r.ID, "uuid-1")
	}
	if r.Type != "product" {
		t.Errorf("Type = %q, want %q", r.Type, "product")
	}
	if r.Title != "Gaming Laptop" {
		t.Errorf("Title = %q, want %q", r.Title, "Gaming Laptop")
	}
	if r.Slug != "gaming-laptop" {
		t.Errorf("Slug = %q, want %q", r.Slug, "gaming-laptop")
	}
}

func TestMeilisearchEngine_Search_WithLocale(t *testing.T) {
	var capturedFilter interface{}
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		searchFn: func(_ string, req *ms.SearchRequest) (*ms.SearchResponse, error) {
			capturedFilter = req.Filter
			return &ms.SearchResponse{}, nil
		},
	}
	client.indexes["stoa_categories"] = &mockIndex{}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	_, err := engine.Search(context.Background(), sdk.SearchRequest{
		Query:  "test",
		Locale: "de-DE",
		Page:   1,
		Limit:  25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	filterArr, ok := capturedFilter.([][]string)
	if !ok {
		t.Fatalf("expected [][]string filter, got %T", capturedFilter)
	}
	if len(filterArr) != 1 || len(filterArr[0]) != 1 || filterArr[0][0] != "locale = de-DE" {
		t.Errorf("Filter = %v, want [[locale = de-DE]]", filterArr)
	}
}

func TestMeilisearchEngine_Search_DefaultPagination(t *testing.T) {
	var capturedReq *ms.SearchRequest
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		searchFn: func(_ string, req *ms.SearchRequest) (*ms.SearchResponse, error) {
			capturedReq = req
			return &ms.SearchResponse{}, nil
		},
	}
	client.indexes["stoa_categories"] = &mockIndex{}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	_, err := engine.Search(context.Background(), sdk.SearchRequest{
		Query: "test",
		Page:  0, // invalid, should default to 1
		Limit: 0, // invalid, should default to 25
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq.Offset != 0 {
		t.Errorf("Offset = %d, want 0", capturedReq.Offset)
	}
	if capturedReq.Limit != 25 {
		t.Errorf("Limit = %d, want 25", capturedReq.Limit)
	}
}

func TestMeilisearchEngine_Search_Error(t *testing.T) {
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		searchFn: func(_ string, _ *ms.SearchRequest) (*ms.SearchResponse, error) {
			return nil, errors.New("connection refused")
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	resp, err := engine.Search(context.Background(), sdk.SearchRequest{
		Query: "test",
		Types: []string{"product"},
	})
	// Search errors are logged but don't fail — results from that index are skipped.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("Results len = %d, want 0", len(resp.Results))
	}
}

func TestMeilisearchEngine_Index(t *testing.T) {
	var gotDocs interface{}
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		addDocumentsFn: func(documents interface{}, _ ...string) (*ms.TaskInfo, error) {
			gotDocs = documents
			return &ms.TaskInfo{TaskUID: 1}, nil
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	data := map[string]interface{}{"name": "Test Product", "price": 1999}
	err := engine.Index(context.Background(), "product", "uuid-1_de-DE", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	docs, ok := gotDocs.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{}, got %T", gotDocs)
	}
	if len(docs) != 1 {
		t.Fatalf("docs len = %d, want 1", len(docs))
	}
	if docs[0]["id"] != "uuid-1_de-DE" {
		t.Errorf("id = %v, want %q", docs[0]["id"], "uuid-1_de-DE")
	}
	if docs[0]["name"] != "Test Product" {
		t.Errorf("name = %v, want %q", docs[0]["name"], "Test Product")
	}
}

func TestMeilisearchEngine_Index_Error(t *testing.T) {
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		addDocumentsFn: func(_ interface{}, _ ...string) (*ms.TaskInfo, error) {
			return nil, errors.New("index failed")
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	err := engine.Index(context.Background(), "product", "uuid-1", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMeilisearchEngine_Remove(t *testing.T) {
	var gotID string
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		deleteDocumentFn: func(id string) (*ms.TaskInfo, error) {
			gotID = id
			return &ms.TaskInfo{TaskUID: 2}, nil
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	err := engine.Remove(context.Background(), "product", "uuid-1_de-DE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotID != "uuid-1_de-DE" {
		t.Errorf("id = %q, want %q", gotID, "uuid-1_de-DE")
	}
}

func TestMeilisearchEngine_Remove_Error(t *testing.T) {
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		deleteDocumentFn: func(_ string) (*ms.TaskInfo, error) {
			return nil, errors.New("delete failed")
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	err := engine.Remove(context.Background(), "product", "uuid-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestMeilisearchEngine_IndexName(t *testing.T) {
	engine := newMeilisearchEngineWithProvider(newMockClient(), Config{IndexPrefix: "shop"}, zerolog.Nop())

	if got := engine.productsIndex(); got != "shop_products" {
		t.Errorf("productsIndex() = %q, want %q", got, "shop_products")
	}
	if got := engine.categoriesIndex(); got != "shop_categories" {
		t.Errorf("categoriesIndex() = %q, want %q", got, "shop_categories")
	}
	if got := engine.indexName("product"); got != "shop_products" {
		t.Errorf("indexName(product) = %q, want %q", got, "shop_products")
	}
}

func TestMeilisearchEngine_Search_MultipleTypes(t *testing.T) {
	client := newMockClient()
	client.indexes["stoa_products"] = &mockIndex{
		searchFn: func(_ string, _ *ms.SearchRequest) (*ms.SearchResponse, error) {
			return &ms.SearchResponse{
				Hits: []interface{}{
					map[string]interface{}{"entity_id": "prod-1", "name": "Product"},
				},
				EstimatedTotalHits: 1,
			}, nil
		},
	}
	client.indexes["stoa_categories"] = &mockIndex{
		searchFn: func(_ string, _ *ms.SearchRequest) (*ms.SearchResponse, error) {
			return &ms.SearchResponse{
				Hits: []interface{}{
					map[string]interface{}{"entity_id": "cat-1", "name": "Category"},
				},
				EstimatedTotalHits: 1,
			}, nil
		},
	}

	engine := newMeilisearchEngineWithProvider(client, Config{IndexPrefix: "stoa"}, zerolog.Nop())

	resp, err := engine.Search(context.Background(), sdk.SearchRequest{
		Query: "test",
		Page:  1,
		Limit: 25,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("Results len = %d, want 2", len(resp.Results))
	}
	if resp.Results[0].Type != "product" {
		t.Errorf("Results[0].Type = %q, want %q", resp.Results[0].Type, "product")
	}
	if resp.Results[1].Type != "category" {
		t.Errorf("Results[1].Type = %q, want %q", resp.Results[1].Type, "category")
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
}
