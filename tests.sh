#!/bin/bash

set -e

BASE_URL="http://localhost:8080/api/v1"
CONTENT_TYPE="Content-Type: application/json"

echo "== 🔨 Creating user =="
create_response=$(curl -s -X POST "$BASE_URL/users" -H "$CONTENT_TYPE" -d '{
  "name": "John Doe",
  "email": "john@example2.com",
  "age": 30
}')
echo "$create_response" | jq .

user_id=$(echo "$create_response" | jq -r '.data.id')

if [[ -z "$user_id" || "$user_id" == "null" ]]; then
  echo "❌ Failed to create user"
  exit 1
fi

echo ""
echo "== 📦 Getting user by ID: $user_id =="
get_response=$(curl -s "$BASE_URL/users/$user_id")
echo "$get_response" | jq .

echo ""
echo "== ✏️ Updating user =="
update_response=$(curl -s -X PUT "$BASE_URL/users/$user_id" -H "$CONTENT_TYPE" -d '{
  "name": "Johnny Updated",
  "email": "johnny.updated@example.com",
  "age": 35
}')
echo "$update_response" | jq .

echo ""
echo "== 📄 Getting paginated user list =="
list_response=$(curl -s "$BASE_URL/users?page=1&limit=5")
echo "$list_response" | jq .

echo ""
echo "== 🗑️ Deleting user =="
delete_response=$(curl -s -X DELETE "$BASE_URL/users/$user_id")
echo "$delete_response" | jq .

echo ""
echo "✅ All tests completed successfully"
