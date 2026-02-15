-- name: DoesUserExist :one
SELECT id FROM users WHERE id = $1;