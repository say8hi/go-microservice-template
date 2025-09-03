-- KEYS[1] = req:{reqID}:objects
-- KEYS[2] = req:{reqID}:remaining
-- ARGV[1] = field (индекс объекта)
-- ARGV[2] = statusType
-- ARGV[3] = новое значение ("true"/"false")

local objectsKey = KEYS[1]
local remainingKey = KEYS[2]
local field = ARGV[1]
local statusType = ARGV[2]
local value = ARGV[3]

-- читаем JSON объекта
local data = redis.call("HGET", objectsKey, field)
if not data then
  return nil
end

local obj = cjson.decode(data)

-- проверяем старое значение
local oldValue = obj[statusType]

-- обновляем только одно поле
if value == "true" then
    obj[statusType] = true
else
    obj[statusType] = false
end

-- если старое значение было nil или false, а новое true → уменьшаем remaining
if (not oldValue or oldValue == false) and obj[statusType] == true then
    redis.call("DECR", remainingKey)
end

-- сохраняем обратно
local newData = cjson.encode(obj)
redis.call("HSET", objectsKey, field, newData)

return newData

