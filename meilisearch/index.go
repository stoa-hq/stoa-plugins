package meilisearch

import (
	"fmt"
	"time"

	ms "github.com/meilisearch/meilisearch-go"
)

// awaitTask waits for a Meilisearch task to complete and returns an error
// if the task did not succeed.
func awaitTask(client clientProvider, taskUID int64) error {
	task, err := client.WaitForTask(taskUID, 100*time.Millisecond)
	if err != nil {
		return err
	}
	if task.Status != ms.TaskStatusSucceeded {
		detail := ""
		if task.Error.Code != "" {
			detail = ": " + task.Error.Code + " — " + task.Error.Message
		}
		return fmt.Errorf("task %d status %s%s", taskUID, task.Status, detail)
	}
	return nil
}

// configureIndexSettings creates or updates the Meilisearch index settings
// for products and categories.
func configureIndexSettings(client clientProvider, prefix string) error {
	productSettings := &ms.Settings{
		SearchableAttributes: []string{"name", "description", "sku"},
		FilterableAttributes: []string{"locale", "active", "category_ids", "price_gross"},
		SortableAttributes:   []string{"price_gross", "created_at", "name"},
	}

	taskInfo, err := client.Index(prefix + "_products").UpdateSettings(productSettings)
	if err != nil {
		return fmt.Errorf("configuring products index settings: %w", err)
	}
	if err := awaitTask(client, taskInfo.TaskUID); err != nil {
		return fmt.Errorf("products settings task: %w", err)
	}

	categorySettings := &ms.Settings{
		SearchableAttributes: []string{"name", "description"},
		FilterableAttributes: []string{"locale", "active", "parent_id"},
		SortableAttributes:   []string{"position", "name"},
	}

	taskInfo, err = client.Index(prefix + "_categories").UpdateSettings(categorySettings)
	if err != nil {
		return fmt.Errorf("configuring categories index settings: %w", err)
	}
	if err := awaitTask(client, taskInfo.TaskUID); err != nil {
		return fmt.Errorf("categories settings task: %w", err)
	}

	return nil
}
