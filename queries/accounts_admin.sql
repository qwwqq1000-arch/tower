-- name: ListNodeAccountsAll :many
SELECT na.node_id, na.account_id, na.profile_id, na.enabled, na.weight, na.role, na.egress,
       n.name AS node_name, n.base_url
FROM node_accounts na
JOIN nodes n ON n.id = na.node_id
ORDER BY n.name, na.profile_id;
