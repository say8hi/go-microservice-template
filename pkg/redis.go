type DataService struct {
	rdb                *redis.Client
	updateStatusScript string
}

type BatchData struct {
	RequestID string     `json:"request_id"`
	UserID    string     `json:"user_id"`
	Items     []DataItem `json:"items"`
}

type DataItem struct {
	ID           string                 `json:"id"`
	OriginalData map[string]interface{} `json:"original_data"`
	EnrichedData map[string]interface{} `json:"enriched_data"`
}

func (ds *DataService) SaveBatchData(ctx context.Context, data BatchData) error {
	// 1. Метаданные
	metadata := map[string]interface{}{
		"total_items":     len(data.Items),
		"completed_items": 0,
		"status":          "processing",
		"user_id":         data.UserID,
		"created_at":      time.Now().Unix(),
	}

	err := ds.rdb.HMSet(ctx, data.RequestID+":metadata", metadata).Err()
	if err != nil {
		return err
	}

	// 2. Элементы
	itemsJSON, _ := json.Marshal(data.Items)
	err = ds.rdb.Set(ctx, data.RequestID+":items", itemsJSON, time.Hour).Err()
	if err != nil {
		return err
	}

	// 3. Статусы
	statuses := make(map[string]interface{})
	for _, item := range data.Items {
		statuses[item.ID] = "pending"
	}

	err = ds.rdb.HMSet(ctx, data.RequestID+":statuses", statuses).Err()
	if err != nil {
		return err
	}

	// 4. TTL
	ds.rdb.Expire(ctx, data.RequestID+":metadata", time.Hour)
	ds.rdb.Expire(ctx, data.RequestID+":statuses", time.Hour)

	return nil
}

func (ds *DataService) UpdateItemStatus(ctx context.Context, reqID, itemID, status string) (string, error) {
	// Lua-скрипт для атомарного обновления
	result, err := ds.rdb.Eval(ctx, ds.updateStatusScript, []string{reqID}, itemID, status).Result()
	if err != nil {
		return "", err
	}

	if result == nil {
		return "", nil // Еще не все завершено
	}

	return result.(string), nil // userID - все завершено
}

func (ds *DataService) GetBatchData(ctx context.Context, reqID string) (*BatchResponse, error) {
	metadata, err := ds.rdb.HGetAll(ctx, reqID+":metadata").Result()
	if err != nil || len(metadata) == 0 {
		return nil, errors.New("request not found")
	}

	itemsRaw, err := ds.rdb.Get(ctx, reqID+":items").Result()
	if err != nil {
		return nil, err
	}

	statuses, err := ds.rdb.HGetAll(ctx, reqID+":statuses").Result()
	if err != nil {
		return nil, err
	}

	var items []DataItem
	json.Unmarshal([]byte(itemsRaw), &items)

	// Объединяем статусы с элементами
	for i := range items {
		items[i].Status = statuses[items[i].ID]
	}

	totalItems, _ := strconv.Atoi(metadata["total_items"])
	completedItems, _ := strconv.Atoi(metadata["completed_items"])

	return &BatchResponse{
		RequestID: reqID,
		Metadata: BatchMetadata{
			TotalItems:     totalItems,
			CompletedItems: completedItems,
			Status:         metadata["status"],
			UserID:         metadata["user_id"],
		},
		Items: items,
	}, nil
}
