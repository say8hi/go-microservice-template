redis.call('HSET', KEYS[1] .. ':statuses', ARGV[1], ARGV[2])

local completed = 0
local statuses = redis.call('HGETALL', KEYS[1] .. ':statuses')
for i = 2, #statuses, 2 do
    if statuses[i] ~= 'pending' then
        completed = completed + 1
    end
end

redis.call('HSET', KEYS[1] .. ':metadata', 'completed_items', completed)

-- Проверяем завершение всех элементов
local total = tonumber(redis.call('HGET', KEYS[1] .. ':metadata', 'total_items'))
if completed >= total then
    redis.call('HSET', KEYS[1] .. ':metadata', 'status', 'completed')
    return redis.call('HGET', KEYS[1] .. ':metadata', 'user_id')
end
return nil
