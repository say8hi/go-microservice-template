package main

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

// Lua script
const updateScript = `
local obj = redis.call("HGET", KEYS[1], ARGV[1])
local data = {}
local cjson = cjson

if obj then
    data = cjson.decode(obj)
else
    data = {}
end

local alreadySet = data[ARGV[2]] ~= nil

if ARGV[3] == "true" then
    data[ARGV[2]] = true
else
    data[ARGV[2]] = false
end

redis.call("HSET", KEYS[1], ARGV[1], cjson.encode(data))

if not alreadySet then
    if data["statusFee"] ~= nil and data["statusNDP"] ~= nil then
        local left = redis.call("DECR", KEYS[2])
        return left
    end
end

return redis.call("GET", KEYS[2])
`

func updateStatus(
	rdb *redis.Client,
	reqID string,
	objectID string,
	statusType string,
	value bool,
) (int64, error) {
	script := redis.NewScript(updateScript)
	keyObjects := fmt.Sprintf("req:%s:objects", reqID)
	keyRemaining := fmt.Sprintf("req:%s:remaining", reqID)

	val := "false"
	if value {
		val = "true"
	}

	res, err := script.Run(ctx, rdb, []string{keyObjects, keyRemaining}, objectID, statusType, val).
		Result()
	if err != nil {
		return -1, err
	}

	// Lua возвращает строку или число, приводим к int64
	switch v := res.(type) {
	case int64:
		return v, nil
	case string:
		// redis возвращает строку для GET
		var n int64
		fmt.Sscan(v, &n)
		return n, nil
	default:
		return -1, fmt.Errorf("unexpected return type %T", v)
	}
}

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	reqID := "123"

	// пример: объект 0 получает коллбэк по statusFee
	left, err := updateStatus(rdb, reqID, "0", "statusFee", true)
	if err != nil {
		panic(err)
	}
	fmt.Println("Remaining:", left)

	// пример: объект 0 получает коллбэк по statusNDP
	left, err = updateStatus(rdb, reqID, "0", "statusNDP", false)
	if err != nil {
		panic(err)
	}
	fmt.Println("Remaining:", left)
