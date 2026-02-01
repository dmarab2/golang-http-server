-- name: GetSingleUser :one
SELECT * FROM users WHERE email = $1;