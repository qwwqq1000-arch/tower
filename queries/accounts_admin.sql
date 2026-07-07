-- name: ListNodeAccountsAll :many
SELECT na.node_id, na.account_id, na.profile_id, na.enabled, na.weight, na.role, na.egress,
       n.name AS node_name, n.base_url,
       coalesce(a.email,'') AS email, coalesce(a.status,'') AS acct_status,
       coalesce(a.expires_at,0) AS expires_at, coalesce(a.subscription_type,'') AS subscription_type,
       coalesce(a.owner_id,'') AS acct_owner_id,
       coalesce(a.no_1m_until,0) AS no_1m_until
FROM node_accounts na
JOIN nodes n ON n.id = na.node_id
LEFT JOIN accounts a ON a.id = na.account_id
ORDER BY n.name, na.profile_id;
