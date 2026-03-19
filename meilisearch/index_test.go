package meilisearch

import (
	"errors"
	"testing"
	"time"

	ms "github.com/meilisearch/meilisearch-go"
)

func TestConfigureIndexSettings_Success(t *testing.T) {
	var productSettingsCalled, categorySettingsCalled bool

	client := newMockClient()
	client.indexes["test_products"] = &mockIndex{
		updateSettingsFn: func(settings *ms.Settings) (*ms.TaskInfo, error) {
			productSettingsCalled = true
			if len(settings.SearchableAttributes) != 3 {
				t.Errorf("products searchable attrs = %d, want 3", len(settings.SearchableAttributes))
			}
			if settings.SearchableAttributes[0] != "name" {
				t.Errorf("products searchable[0] = %q, want %q", settings.SearchableAttributes[0], "name")
			}
			if len(settings.FilterableAttributes) != 4 {
				t.Errorf("products filterable attrs = %d, want 4", len(settings.FilterableAttributes))
			}
			if len(settings.SortableAttributes) != 3 {
				t.Errorf("products sortable attrs = %d, want 3", len(settings.SortableAttributes))
			}
			return &ms.TaskInfo{}, nil
		},
	}
	client.indexes["test_categories"] = &mockIndex{
		updateSettingsFn: func(settings *ms.Settings) (*ms.TaskInfo, error) {
			categorySettingsCalled = true
			if len(settings.SearchableAttributes) != 2 {
				t.Errorf("categories searchable attrs = %d, want 2", len(settings.SearchableAttributes))
			}
			if len(settings.FilterableAttributes) != 3 {
				t.Errorf("categories filterable attrs = %d, want 3", len(settings.FilterableAttributes))
			}
			if len(settings.SortableAttributes) != 2 {
				t.Errorf("categories sortable attrs = %d, want 2", len(settings.SortableAttributes))
			}
			return &ms.TaskInfo{}, nil
		},
	}

	err := configureIndexSettings(client, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !productSettingsCalled {
		t.Error("product settings were not configured")
	}
	if !categorySettingsCalled {
		t.Error("category settings were not configured")
	}
}

func TestConfigureIndexSettings_ProductsError(t *testing.T) {
	client := newMockClient()
	client.indexes["test_products"] = &mockIndex{
		updateSettingsFn: func(_ *ms.Settings) (*ms.TaskInfo, error) {
			return nil, errors.New("connection refused")
		},
	}

	err := configureIndexSettings(client, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestConfigureIndexSettings_CategoriesError(t *testing.T) {
	client := newMockClient()
	client.indexes["test_products"] = &mockIndex{
		updateSettingsFn: func(_ *ms.Settings) (*ms.TaskInfo, error) {
			return &ms.TaskInfo{}, nil
		},
	}
	client.indexes["test_categories"] = &mockIndex{
		updateSettingsFn: func(_ *ms.Settings) (*ms.TaskInfo, error) {
			return nil, errors.New("connection refused")
		},
	}

	err := configureIndexSettings(client, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAwaitTask_Success(t *testing.T) {
	client := newMockClient()
	client.waitForTaskFn = func(_ int64, _ time.Duration) (*ms.Task, error) {
		return &ms.Task{Status: ms.TaskStatusSucceeded}, nil
	}

	if err := awaitTask(client, 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAwaitTask_FailedTask(t *testing.T) {
	client := newMockClient()
	client.waitForTaskFn = func(_ int64, _ time.Duration) (*ms.Task, error) {
		return &ms.Task{Status: ms.TaskStatusFailed}, nil
	}

	err := awaitTask(client, 1)
	if err == nil {
		t.Fatal("expected error for failed task, got nil")
	}
}

func TestConfigureIndexSettings_WaitForTaskError(t *testing.T) {
	client := newMockClient()
	client.indexes["test_products"] = &mockIndex{
		updateSettingsFn: func(_ *ms.Settings) (*ms.TaskInfo, error) {
			return &ms.TaskInfo{TaskUID: 42}, nil
		},
	}
	client.waitForTaskFn = func(_ int64, _ time.Duration) (*ms.Task, error) {
		return nil, errors.New("task failed")
	}

	err := configureIndexSettings(client, "test")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
