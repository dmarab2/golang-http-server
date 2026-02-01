-- name: GetSingleChirp :one
SELECT * FROM chirps WHERE id = $1;