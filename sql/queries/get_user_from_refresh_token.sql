-- name: GetSingleUserFromRefreshToken :one

SELECT * FROM users INNER JOIN refresh_tokens ON users.id = refresh_tokens.user_id WHERE refresh_tokens.token = $1;