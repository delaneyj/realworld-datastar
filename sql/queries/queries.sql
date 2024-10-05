-- name: UserByEmail :one
SELECT
    *
FROM
    users
WHERE
    email = @email;

-- name: TopTags :many
SELECT
    t.name,
    count(*) count
FROM
    article_tags at
    INNER JOIN tags t ON t.id = at.tag_id
GROUP BY
    tag_id
ORDER BY
    count DESC
LIMIT
    @limit;

-- name: YourFeedArticlePreviews :many
SELECT
    a.id AS article_id,
    u.id AS author_id,
    u.username,
    u.image_url,
    a.title,
    a.description
FROM
    following f
    INNER JOIN articles a ON a.author_id = f.follows_id
    INNER JOIN users u ON u.id = a.author_id
WHERE
    user_id = @userID
ORDER BY
    a.updated_at DESC,
    a.id DESC
LIMIT
    @limit OFFSET @offset;

-- name: ArticlePreviewsByAuthor :many
SELECT
    a.id AS article_id,
    u.id AS author_id,
    u.username,
    u.image_url,
    a.title,
    a.description
FROM
    articles a
    INNER JOIN users u ON u.id = a.author_id
WHERE
    a.author_id = @authorID
ORDER BY
    a.updated_at DESC,
    a.id DESC
LIMIT
    @limit OFFSET @offset;

-- name: ArticleCountByAuthor :one
SELECT
    count(*)
FROM
    articles
WHERE
    author_id = @authorID;

-- name: ArticlePreviewsByFavoriter :many
SELECT
    a.id AS article_id,
    u.id AS author_id,
    u.username,
    u.image_url,
    a.title,
    a.description
FROM
    article_favorites af
    INNER JOIN articles a ON a.id = af.article_id
    INNER JOIN users u ON u.id = a.author_id
WHERE
    af.user_id = @favoriterID
ORDER BY
    a.created_at DESC
LIMIT
    @limit OFFSET @offset;

-- name: ArticleCountByFavoriter :one
SELECT
    count(*)
FROM
    article_favorites
WHERE
    user_id = @favoriterID;

-- name: TagsForArticle :many
SELECT
    t.id,
    t.name
FROM
    tags t
    INNER JOIN article_tags AS at ON at.tag_id = t.id
WHERE
    at.article_id = @articleID
ORDER BY
    t.name;

-- name: ArticleFavoriteCount :one
SELECT
    count(*)
FROM
    article_favorites
WHERE
    article_id = @articleID;

-- name: ArticleComments :many
SELECT
    c.id AS comment_id,
    u.id AS commenter_id,
    u.username AS commenter_name,
    u.image_url AS commenter_image,
    c.body,
    c.created_at
FROM
    comments c
    INNER JOIN users u ON u.id = c.author_id
WHERE
    c.article_id = @articleID
ORDER BY
    c.created_at DESC;

-- name: IsUserFollowing :one
SELECT
    count(*) > 0
FROM
    following
WHERE
    user_id = @userID
    AND follows_id = @followsID;

-- name: HasUserFavorited :one
SELECT
    count(*) > 0
FROM
    article_favorites
WHERE
    user_id = @userID
    AND article_id = @articleID;

-- name: DeleteFavoritedArticle :exec
DELETE FROM
    article_favorites
WHERE
    user_id = @userID
    AND article_id = @articleID;

-- name: DeleteFollow :exec
DELETE FROM
    following
WHERE
    user_id = @userID
    AND follows_id = @followsID;

-- name: TagByName :one
SELECT
    *
FROM
    tags
WHERE
    name = @name;

-- name: DeleteTagFromArticle :exec
DELETE FROM
    article_tags
WHERE
    article_id = @articleID
    AND tag_id = @tagID;